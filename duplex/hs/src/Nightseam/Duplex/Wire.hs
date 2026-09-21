{-# LANGUAGE DeriveDataTypeable #-}
{-# LANGUAGE OverloadedStrings #-}
-- | Carrier-neutral frames and relative path views. A root owns admission,
-- bounded dispatch, correlation and closure; views allocate no carrier or queue.
module Nightseam.Duplex.Wire
  ( Path, Message (..), ReturnAddress, newReturnAddress, returnWire
  , Receiver (..), Wire (..), WireError (..), at, mount, encodePath, decodePath
  ) where

import Control.Concurrent.STM
import Control.Exception (Exception, mask, onException, throwIO)
import Control.Monad (forM_, unless, when)
import Data.Aeson (Value)
import qualified Data.ByteString as BS
import Data.Dynamic (Dynamic)
import qualified Data.Map.Strict as Map
import qualified Data.Set as Set
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import Data.Typeable (Typeable)
import Data.Unique (Unique, newUnique)

type Path = [Text]

-- | Local identity is independent of the representation of the destination.
-- Neither this capability nor a received context is part of a serialized frame.
data ReturnAddress = ReturnAddress Unique Wire

instance Eq ReturnAddress where
  ReturnAddress left _ == ReturnAddress right _ = left == right

newReturnAddress :: Wire -> IO ReturnAddress
newReturnAddress wire = (`ReturnAddress` wire) <$> newUnique

returnWire :: ReturnAddress -> Wire
returnWire (ReturnAddress _ wire) = wire

-- | The frame contains profile fields except the method/event path supplied to
-- 'wireSend'. Runtime keeps its verified incoming context in 'messageContext'.
data Message = Message
  { messageFrame :: Value
  , messageReturn :: Maybe ReturnAddress
  , messageContext :: Maybe Dynamic
  }

data Receiver = Receiver
  { receiverNamespace :: Bool
  , receiverMessage :: Path -> Message -> IO ()
  , receiverClosed :: Int -> Text -> IO ()
  }

data Wire = Wire
  { wireSend :: Path -> Message -> IO ()
  , wireReceive :: Path -> Receiver -> IO (IO ())
  , wireClose :: Int -> Text -> IO ()
  }

data WireError = InvalidPath | NoRoute | ReceiverExists | WireClosed
  deriving (Eq, Show, Typeable)

instance Exception WireError

-- | Encode opaque Unicode scalar segments by their UTF-8 byte lengths.
encodePath :: Path -> Either WireError Text
encodePath path
  | any (T.any (\c -> c >= '\xD800' && c <= '\xDFFF')) path = Left InvalidPath
  | otherwise = Right (T.concat (map segment path))
  where segment value = T.pack (show (BS.length (TE.encodeUtf8 value))) <> ":" <> value

-- | Accept only the canonical encoding. No slash, dot or Unicode normalization
-- is performed. Decode each segment separately so a split code point is refused.
decodePath :: Text -> Either WireError Path
decodePath = go . TE.encodeUtf8
  where
    go input
      | BS.null input = Right []
      | otherwise = do
          let (digits, suffix) = BS.break (== 58) input
          when (BS.null digits || BS.null suffix || BS.any (\c -> c < 48 || c > 57) digits
                || (BS.length digits > 1 && BS.head digits == 48)) (Left InvalidPath)
          let rest = BS.tail suffix
              maximumLength = BS.length rest
              maximumDigits = BS.length (TE.encodeUtf8 (T.pack (show maximumLength)))
          -- Refuse oversized declarations before parsing, including arbitrarily
          -- large integers, and keep all arithmetic inside a bounded Int.
          when (BS.length digits > maximumDigits) (Left InvalidPath)
          let lengthInteger = BS.foldl' (\n c -> n * 10 + fromIntegral (c - 48)) (0 :: Integer) digits
          when (lengthInteger > fromIntegral maximumLength) (Left InvalidPath)
          let (bytes, remaining) = BS.splitAt (fromIntegral lengthInteger) rest
          value <- either (const (Left InvalidPath)) Right (TE.decodeUtf8' bytes)
          (value :) <$> go remaining

-- | Selecting an origin is a pure view; closing it closes its underlying wire.
at :: Wire -> Path -> Wire
at root prefix = Wire
  { wireSend = \path -> wireSend root (prefix ++ path)
  , wireReceive = \path receiver -> wireReceive root (prefix ++ path) receiver
      { receiverMessage = \delivered -> receiverMessage receiver (drop (length prefix) delivered) }
  , wireClose = wireClose root
  }

data Registration = Registration
  { registrationID :: Integer
  , registrationReceiver :: Receiver
  , registrationActive :: TVar Bool
  , registrationDetaches :: TVar [IO ()]
  }

data MountState = MountState
  { mountClosed :: Bool
  , mountNext :: Integer
  , mountRegistrations :: Map.Map Integer Registration
  }

-- | Mount consumes one segment. Empty keys are valid, while the origin itself
-- has no leaf. Closure detaches registrations and leaves borrowed children open.
mount :: Map.Map Text Wire -> IO Wire
mount children = do
  state <- newTVarIO (MountState False 0 Map.empty)
  let destination path = do
        either throwIO (const (pure ())) (encodePath path)
        atomically $ do
          current <- readTVar state
          when (mountClosed current) (throwSTM WireClosed)
          case path of
            key : rest -> maybe (throwSTM NoRoute) (\child -> pure (key, rest, child)) (Map.lookup key children)
            [] -> throwSTM NoRoute
      reserve receiver = atomically $ do
        current <- readTVar state
        when (mountClosed current) (throwSTM WireClosed)
        active <- newTVar True
        detaches <- newTVar []
        let identifier = mountNext current
            registration = Registration identifier receiver active detaches
        writeTVar state current
          { mountNext = identifier + 1
          , mountRegistrations = Map.insert identifier registration (mountRegistrations current)
          }
        pure registration
      remove registration notify code reason = do
        held <- atomically $ do
          active <- readTVar (registrationActive registration)
          if not active then pure Nothing else do
            writeTVar (registrationActive registration) False
            modifyTVar' state (\s -> s {mountRegistrations = Map.delete (registrationID registration) (mountRegistrations s)})
            detaches <- readTVar (registrationDetaches registration)
            writeTVar (registrationDetaches registration) []
            pure (Just detaches)
        case held of
          Nothing -> pure ()
          Just detaches -> do
            sequence_ detaches
            when notify (receiverClosed (registrationReceiver registration) code reason)
      attach registration child path receiver = mask $ \restore -> do
        detach <- restore (wireReceive child path receiver)
        active <- atomically $ do
          live <- readTVar (registrationActive registration)
          when live (modifyTVar' (registrationDetaches registration) (detach :))
          pure live
        unless active (detach >> throwIO WireClosed)
      receiveOne path receiver = mask $ \restore -> do
        (key, rest, child) <- destination path
        registration <- reserve receiver
        let forget = remove registration False 0 ""
            forwarded = receiver
              { receiverMessage = \delivered -> receiverMessage receiver (key : delivered)
              , receiverClosed = remove registration True
              }
        restore (attach registration child rest forwarded) `onException` forget
        pure forget
      receiveNamespace receiver = mask $ \restore -> do
        registration <- reserve receiver
        remaining <- newTVarIO (Map.keysSet children)
        let forget = remove registration False 0 ""
            childEnded key code reason = do
              lastChild <- atomically $ do
                old <- readTVar remaining
                let next = Set.delete key old
                writeTVar remaining next
                pure (Set.member key old && Set.null next)
              when lastChild (remove registration True code reason)
            install (key, child) = attach registration child [] receiver
              { receiverNamespace = True
              , receiverMessage = \delivered -> receiverMessage receiver (key : delivered)
              , receiverClosed = childEnded key
              }
        restore (mapM_ install (Map.toList children)) `onException` forget
        pure forget
      close code reason = mask $ \_ -> do
        registrations <- atomically $ do
          current <- readTVar state
          if mountClosed current then pure [] else do
            let held = Map.elems (mountRegistrations current)
            writeTVar state current {mountClosed = True}
            pure held
        forM_ registrations (\registration -> remove registration True code reason)
  pure Wire
    { wireSend = \path message -> do
        (_, rest, child) <- destination path
        wireSend child rest message
    , wireReceive = \path receiver -> if null path && receiverNamespace receiver
        then receiveNamespace receiver else receiveOne path receiver
    , wireClose = close
    }
