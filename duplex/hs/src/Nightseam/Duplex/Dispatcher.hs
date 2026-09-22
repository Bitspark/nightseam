{-# LANGUAGE OverloadedStrings #-}
module Nightseam.Duplex.Dispatcher
  (Dispatcher, newDispatcher, dispatcherWire, register, registerPrefix, selectEndpoint, closeDispatcher) where

import Bitwire
import Control.Concurrent.MVar
import Control.Concurrent.STM
import Control.Exception (mask, onException, throwIO)
import Control.Monad (forM_, unless, when)
import qualified Data.Map.Strict as M
import Data.Maybe (catMaybes, listToMaybe)
import Data.List (sortOn)
import Data.Text (Text)
import Data.Unique (Unique, newUnique)
import Nightseam.Duplex.Wire
import System.Mem.Weak

data Registration = Registration Unique Path Receiver
data State = State Bool (M.Map (Path, Bool) Registration)
data Dispatcher = Dispatcher Endpoint (TVar State) (MVar [Weak (ReturnAddress, Text, Registration)]) (IO ())

newDispatcher :: Endpoint -> IO Dispatcher
newDispatcher endpoint = mask $ \restore -> do
  state <- newTVarIO (State False M.empty)
  captured <- newMVar []
  let dispatch path message = do
        State ended routes <- readTVarIO state
        unless ended $ do
          let target = case M.lookup (path, False) routes of
                Just exact -> Just exact
                Nothing -> listToMaybe [r | ((prefix, namespace), r) <- reverse (sortOn (length . fst . fst) (M.toList routes)), namespace, prefix == take (length prefix) path]
          selected <- modifyMVar captured $ \old -> do
            live <- fmap catMaybes $ mapM (\weak -> fmap ((,) weak) <$> deRefWeak weak) old
            case (frameBody (messageFrame message), messageReturn message) of
              (Cancel ident, Just address) -> pure
                ([weak | (weak,(a,i,_)) <- live, a /= address || i /= ident], listToMaybe [r | (_, (a,i,r)) <- live, a == address, i == ident])
              (Request ident _ _, Just address) | Just registration <- target -> do
                weak <- mkWeak (returnWire address) (address, ident, registration) Nothing
                pure (weak : map fst live, target)
              _ -> pure (map fst live, target)
          case selected of
            Just (Registration _ _ receiver) | Just callback <- onMessage receiver -> callback path message
            _ -> case (frameBody (messageFrame message), messageReturn message) of
              (Request ident _ _, Just address) -> send (returnWire address) []
                (Message (ProfileFrame (frameTrace (messageFrame message)) (Response ident (Left (ProfileError "method_not_found" "Unknown method" Nothing)))) Nothing)
              _ -> pure ()
      finish code reason = do
        held <- atomically $ do
          State ended routes <- readTVar state
          writeTVar state (State True M.empty)
          pure (if ended then [] else M.elems routes)
        modifyMVar_ captured (const (pure []))
        forM_ held $ \(Registration _ _ receiver) -> forM_ (onClosed receiver) (\f -> f code reason)
  detach <- restore (receive endpoint (Receiver (Just dispatch) (Just finish)))
  State ended _ <- readTVarIO state
  when ended (detach >> throwIO WireClosed)
  pure (Dispatcher endpoint state captured detach)

dispatcherWire :: Dispatcher -> Wire
dispatcherWire (Dispatcher endpoint state _ _) = Wire $ \path message -> do
  State ended _ <- readTVarIO state
  when ended (throwIO WireClosed)
  send (endpointWire endpoint) path message

register :: Dispatcher -> Path -> Receiver -> IO (IO ())
register dispatcher = registerRoute dispatcher False
registerPrefix :: Dispatcher -> Path -> Receiver -> IO (IO ())
registerPrefix dispatcher = registerRoute dispatcher True
registerRoute :: Dispatcher -> Bool -> Path -> Receiver -> IO (IO ())
registerRoute (Dispatcher _ state _ _) prefix path receiver = do
  _ <- either throwIO pure (encodePath path)
  key <- newUnique
  let route = (path, prefix)
  atomically $ do
    State ended routes <- readTVar state
    when ended (throwSTM WireClosed)
    when (M.member route routes) (throwSTM ReceiverExists)
    writeTVar state (State ended (M.insert route (Registration key path receiver) routes))
  pure $ atomically $ modifyTVar' state $ \(State ended routes) -> State ended $ case M.lookup route routes of
    Just (Registration held _ _) | held == key -> M.delete route routes
    _ -> routes

selectEndpoint :: Dispatcher -> Path -> IO Endpoint
selectEndpoint owner prefix = do
  state <- newMVar (False, Nothing)
  let end code reason = do
        held <- modifyMVar state $ \(ended, attachment) -> pure ((True, Nothing), if ended then Nothing else attachment)
        forM_ held $ \(_, receiver, detach) -> detach >> forM_ (onClosed receiver) (\f -> f code reason)
      attach receiver = mask $ \restore -> do
        key <- newUnique
        active <- newTVarIO True
        -- Reserve before the upstream acquisition, allowing close during it.
        modifyMVar_ state $ \(ended, held) -> do
          when ended (throwIO WireClosed)
          unless (case held of Nothing -> True; _ -> False) (throwIO ReceiverExists)
          pure (False, Just (key, receiver, atomically (writeTVar active False)))
        let forget = do
              held <- modifyMVar state $ \(ended, held) -> case held of
                Just (current, _, _) | current == key -> pure ((ended, Nothing), held)
                _ -> pure ((ended, held), Nothing)
              forM_ held (\(_, _, off) -> off)
        detach <- restore (registerPrefix owner prefix receiver
          { onMessage = (\f path -> f (drop (length prefix) path)) <$> onMessage receiver
          , onClosed = Just end }) `onException` forget
        accepted <- modifyMVar state $ \(ended, held) -> case held of
          Just (current, _, _) | current == key -> pure ((ended, Just (key, receiver, detach)), True)
          _ -> pure ((ended, held), False)
        unless accepted (detach >> throwIO WireClosed)
        pure forget
  pure Endpoint
    { endpointWire = Wire $ \path message -> do
        (ended, _) <- readMVar state
        when ended (throwIO WireClosed)
        send (dispatcherWire owner) (prefix ++ path) message
    , receive = attach, close = end }

closeDispatcher :: Dispatcher -> Code -> Text -> IO ()
closeDispatcher (Dispatcher _ state captured detach) code reason = do
  held <- atomically $ do
    State ended routes <- readTVar state
    writeTVar state (State True M.empty)
    pure (if ended then [] else M.elems routes)
  detach
  modifyMVar_ captured (const (pure []))
  forM_ held $ \(Registration _ _ receiver) -> forM_ (onClosed receiver) (\f -> f code reason)
