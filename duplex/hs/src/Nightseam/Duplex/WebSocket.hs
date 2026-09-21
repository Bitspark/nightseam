{-# LANGUAGE OverloadedStrings #-}
{-# LANGUAGE ScopedTypeVariables #-}
module Nightseam.Duplex.WebSocket
  (Listener, listen, listenerURL, accept, dial, closeListener) where

import Control.Concurrent.Async
import Control.Concurrent.STM
import Control.Exception
import Control.Monad
import qualified Data.ByteString as B
import qualified Data.ByteString.Char8 as BC
import qualified Data.ByteString.Lazy as L
import Data.IORef
import Data.List (find)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Text.Encoding.Error as TE
import qualified Network.Socket as N
import qualified Network.Socket.ByteString as NB
import Network.URI (URI(..), URIAuth(..), parseURI)
import qualified Network.WebSockets as W
import qualified Network.WebSockets.Stream as WS
import Nightseam.Duplex
import System.Timeout

data Listener = Listener N.Socket (Async ()) (TBQueue Connection) (TVar (Maybe N.Socket)) Text

listenerURL :: Listener -> Text
listenerURL (Listener _ _ _ _ url) = url

options :: Int -> W.ConnectionOptions
options limit = W.defaultConnectionOptions
  { W.connectionFramePayloadSizeLimit = W.SizeLimit (fromIntegral (max 1 limit))
  , W.connectionMessageDataSizeLimit = W.SizeLimit (fromIntegral (max 1 limit))
  }

listen :: Int -> [Text] -> IO Listener
listen limit protocols = N.withSocketsDo $ do
  sock <- W.makeListenSocket "127.0.0.1" 0
  address <- N.getSocketName sock
  let port = case address of N.SockAddrInet p _ -> p; N.SockAddrInet6 p _ _ _ -> p; _ -> 0
  queue <- newTBQueueIO 64
  active <- newTVarIO Nothing
  worker <- async $ forever $ do
    (socket, _) <- N.accept sock
    atomically (writeTVar active (Just socket))
    let handshake = do
          pending <- W.makePendingConnection socket (options limit)
          let offered = W.getRequestSubprotocols (W.pendingRequest pending)
              chosen = find (`elem` offered) (map TE.encodeUtf8 protocols)
          ws <- W.acceptRequestWith pending W.defaultAcceptRequest { W.acceptSubprotocol = chosen }
          conn <- wrap socket ws (maybe "" TE.decodeUtf8 chosen)
          admitted <- atomically $ do
            full <- isFullTBQueue queue
            if full then pure False else writeTBQueue queue conn >> pure True
          unless admitted (abortConnection conn)
    result <- try (timeout 30000000 handshake)
    atomically (writeTVar active Nothing)
    case result of
      Right (Just ()) -> pure ()
      _ -> N.close socket
    case result of
      Left (e :: SomeException) | Just (_ :: AsyncException) <- fromException e -> throwIO e
      _ -> pure ()
  pure (Listener sock worker queue active ("ws://127.0.0.1:" <> T.pack (show port)))

accept :: Listener -> IO Connection
accept (Listener _ _ queue _ _) = atomically (readTBQueue queue)

closeListener :: Listener -> IO ()
closeListener (Listener sock worker queue active _) = do
  N.close sock
  readTVarIO active >>= mapM_ N.close
  cancel worker
  pending <- atomically $ let drain = do
                               next <- tryReadTBQueue queue
                               maybe (pure []) (\x -> (x:) <$> drain) next
                         in drain
  mapM_ abortConnection pending

dial :: Text -> Int -> [Text] -> IO Connection
dial url limit protocols = N.withSocketsDo $ do
  uri <- maybe (throwIO (userError "invalid WebSocket URL")) pure (parseURI (T.unpack url))
  when (uriScheme uri /= "ws:") (throwIO (userError "expected ws URL"))
  authority <- maybe (throwIO (userError "missing WebSocket authority")) pure (uriAuthority uri)
  let host = uriRegName authority
      port = case uriPort authority of ':' : p -> p; _ -> "80"
      path = (if null (uriPath uri) then "/" else uriPath uri) <> uriQuery uri
  addresses <- N.getAddrInfo (Just N.defaultHints { N.addrSocketType = N.Stream }) (Just host) (Just port)
  address <- case addresses of a:_ -> pure a; _ -> throwIO (userError "no address")
  socket <- N.socket (N.addrFamily address) N.Stream N.defaultProtocol
  flip onException (N.close socket) $ do
    N.connect socket (N.addrAddress address)
    captured <- newIORef B.empty
    handshake <- newIORef True
    stream <- WS.makeStream
      (do bytes <- NB.recv socket 32768
          active <- readIORef handshake
          when active $ modifyIORef' captured (<> bytes)
          pure (if B.null bytes then Nothing else Just bytes))
      (maybe (N.close socket) (NB.sendAll socket . L.toStrict))
    let headers = if null protocols then [] else [("Sec-WebSocket-Protocol", TE.encodeUtf8 (T.intercalate ", " protocols))]
    ws <- W.newClientConnection stream host path (options limit) headers
    writeIORef handshake False
    response <- readIORef captured
    let header = find (BC.isPrefixOf "sec-websocket-protocol:" . BC.map lower) (BC.lines response)
        lower c | c >= 'A' && c <= 'Z' = toEnum (fromEnum c + 32)
                | otherwise = c
        selected = maybe "" (T.strip . TE.decodeUtf8 . B.drop 1 . BC.dropWhile (/= ':')) header
    when (not (T.null selected) && selected `notElem` protocols) (throwIO (userError "unoffered subprotocol"))
    when (not (null protocols) && T.null selected) (throwIO (userError "no subprotocol selected"))
    wrap socket ws selected

wrap :: N.Socket -> W.Connection -> Text -> IO Connection
wrap socket ws selected = do
  ended <- newEmptyTMVarIO
  let mark e = atomically (void (tryPutTMVar ended e))
      ensureOpen = atomically (tryReadTMVar ended) >>= maybe (pure ()) throwIO
      stop code reason = do
        fresh <- atomically (tryPutTMVar ended (CloseError code reason))
        when fresh $ do
          (unless (code == 1006) (W.sendCloseCode ws (fromIntegral code) reason) `catch` \(_ :: W.ConnectionException) -> pure ()) `finally` N.close socket
      translate action = action `catch` \(err :: W.ConnectionException) -> do
        let e = case err of
              W.CloseRequest code reason -> CloseError (fromIntegral code) (TE.decodeUtf8With TE.lenientDecode (L.toStrict reason))
              W.ConnectionClosed -> CloseError 1006 ""
              _ -> CloseError 1009 "frame rejected"
        mark e
        N.close socket
        throwIO e
      readNext = do
        ensureOpen
        translate $ do
          msg <- W.receiveDataMessage ws
          pure $ case msg of
            W.Text bytes _ -> Frame TextFrame (L.toStrict bytes)
            W.Binary bytes -> Frame BinaryFrame (L.toStrict bytes)
      writeNext (Frame kind bytes) = do
        ensureOpen
        translate $ case kind of
          TextFrame -> W.sendTextData ws bytes
          BinaryFrame -> W.sendBinaryData ws bytes
  pure Connection
    { sendFrame = writeNext, receiveFrame = readNext
    , closeConnection = stop
    , abortConnection = mark (CloseError 1006 "") >> N.close socket
    , connectionSubprotocol = selected
    }
