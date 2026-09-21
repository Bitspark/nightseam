{-# LANGUAGE DeriveDataTypeable #-}
{-# LANGUAGE OverloadedStrings #-}
{-# LANGUAGE ScopedTypeVariables #-}
module Main where

import Control.Concurrent.Async
import Control.Concurrent.STM
import Control.Exception hiding (handle, Handler)
import Control.Monad
import Data.Aeson hiding (Options, defaultOptions)
import qualified Data.Aeson.Key as K
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString as B
import qualified Data.ByteString.Base64 as Base64
import qualified Data.ByteString.Lazy as L
import qualified Data.Map.Strict as M
import Data.Maybe
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Data.Text.Encoding.Error as TE
import Data.Typeable
import qualified Data.Vector as V
import Nightseam.Duplex
import qualified Nightseam.Duplex.WebSocket as WS
import Nightseam.Runtime
import Nightseam.Runtime.Identity
import RecordedWire
import System.IO
import System.Timeout

data DriverError = DriverError Value deriving (Show,Typeable)
instance Exception DriverError
err :: Text -> Text -> IO a
err code message = throwIO (DriverError (object ["code" .= code, "message" .= message]))
get :: Text -> Value -> Value
get key (Object values) = fromMaybe Null (KM.lookup (K.fromText key) values)
get _ _ = Null
text :: Value -> Text
text (String s) = s
text _ = ""
num :: Int -> Value -> Int
num _ (Number n) = round n
num def _ = def
strings :: Value -> [Text]
strings (Array values) = [s | String s <- V.toList values]
strings _ = []
empty :: Value
empty = object []

type Inbox = TVar [Value]
put :: Inbox -> Value -> IO ()
put inbox value = atomically (modifyTVar' inbox (++ [value]))
takeMatching :: Inbox -> (Value -> Bool) -> IO Value
takeMatching inbox predicate = atomically $ do
  values <- readTVar inbox
  case break predicate values of
    (before, x:after) -> writeTVar inbox (before ++ after) >> pure x
    _ -> retry

data ControlledConnection = ControlledConnection
  { native :: Connection, lazy :: Bool, incoming :: TQueue Frame
  , connectionEnded :: TMVar CloseError, reader :: Maybe (Async ()) }
data ControlledPeer = ControlledPeer { nativePeer :: Peer, eventInbox :: Inbox, requestInbox :: Inbox }
data Held = Conn ControlledConnection | Listen WS.Listener (Maybe Options) | Peer ControlledPeer | Call CallContext (Async (Either Value Value))
data Testee = Testee (TVar Int) (TVar (M.Map Text Held))

mint :: Testee -> Held -> IO Text
mint (Testee next handles) value = atomically $ do
  n <- (+1) <$> readTVar next
  writeTVar next n
  let key = "h" <> T.pack (show n)
  modifyTVar' handles (M.insert key value)
  pure key
lookupHeld :: Testee -> Value -> IO Held
lookupHeld (Testee _ handles) args = do
  value <- M.lookup (text (get "on" args)) <$> readTVarIO handles
  maybe (err "unknown_handle" "unknown handle") pure value
connOf :: Testee -> Value -> IO ControlledConnection
connOf t args = lookupHeld t args >>= \value -> case value of Conn c -> pure c; _ -> err "invalid" "expected connection"
peerOf :: Testee -> Value -> IO ControlledPeer
peerOf t args = lookupHeld t args >>= \value -> case value of Peer p -> pure p; _ -> err "invalid" "expected peer"

wrapped :: Connection -> Bool -> IO ControlledConnection
wrapped connection isLazy = do
  queue <- newTQueueIO
  ended <- newEmptyTMVarIO
  let pump = forever (receiveFrame connection >>= atomically . writeTQueue queue)
      recordEnd (e :: SomeException) = atomically (void (tryPutTMVar ended (fromMaybe (CloseError 1006 "") (fromException e))))
  worker <- if isLazy then pure Nothing else Just <$> async (pump `catch` recordEnd)
  pure (ControlledConnection connection isLazy queue ended worker)
receiveControlled :: ControlledConnection -> IO Frame
receiveControlled c = if lazy c then receiveFrame (native c) `catch` \(e :: CloseError) -> do
  atomically (void (tryPutTMVar (connectionEnded c) e))
  throwIO e
  else atomically (readTQueue (incoming c) `orElse` (readTMVar (connectionEnded c) >>= throwSTM))
adopt :: Peer -> IO ControlledPeer
adopt peer = do
  events <- newTVarIO []
  requests <- newTVarIO []
  _ <- onEvent peer $ \ctx name value -> put events (object (["name" .= name, "data" .= value] ++ if M.null (contextReceivedMeta ctx) then [] else ["meta" .= contextReceivedMeta ctx]))
  pure (ControlledPeer peer events requests)
reset :: Testee -> IO ()
reset (Testee _ handles) = do
  held <- atomically $ do values <- readTVar handles; writeTVar handles M.empty; pure (M.elems values)
  forM_ held $ \value -> (case value of
    Conn c -> do abortConnection (native c); mapM_ cancel (reader c)
    Listen listener _ -> WS.closeListener listener
    Peer p -> closePeer (nativePeer p)
    Call ctx worker -> cancelContext ctx >> cancel worker) `catch` \(_ :: SomeException) -> pure ()

within :: Value -> IO a -> IO a
within args action = timeout (1000 * num 5000 (get "within_ms" args)) action >>= maybe (err "timeout" "operation deadline") pure

options :: Value -> IO Options
options args = do
  let o = get "options" args
  when (get "observe" o == Bool True) (err "unsupported" "observer")
  pure defaultOptions
    { maxFrameBytes = num 1048576 (get "max_frame_bytes" o)
    , maxPendingRequests = num 128 (get "max_pending_requests" o)
    , maxConcurrentHandlers = num 64 (get "max_concurrent_handlers" o)
    , queueCapacity = num 128 (get "queue_capacity" o)
    , requestTimeoutMs = num 30000 (get "request_timeout_ms" o)
    , writeTimeoutMs = num 10000 (get "write_timeout_ms" o) }

context :: Value -> IO CallContext
context args = do
  ctx <- newCallContext
  let meta = case get "meta" args of Object values -> M.fromList [(K.toText k,v) | (k,String v) <- KM.toList values]; _ -> M.empty
  pure ctx { contextMeta = meta, contextTimeoutMs = num 0 (get "timeout_ms" args) }

asError :: SomeException -> Value
asError exception = case fromException exception of
  Just (DriverError value) -> value
  Nothing -> case fromException exception of
    Just e -> publicErrorValue e
    Nothing -> case fromException exception of
      Just (CloseError code reason) -> object ["code" .= ("closed"::Text), "message" .= ("connection closed"::Text), "close_code" .= code, "reason" .= reason]
      Nothing -> object ["code" .= ("failed"::Text), "message" .= T.pack (displayException exception)]

handler :: ControlledPeer -> Text -> Value -> Handler
handler p method behavior ctx remote params = do
  put (requestInbox p) (object (["method" .= method, "phase" .= ("started"::Text)] ++ if M.null (contextReceivedMeta ctx) then [] else ["meta" .= contextReceivedMeta ctx]))
  result <- try $ case text (get "kind" behavior) of
    "echo" -> pure params
    "return" -> pure (get "value" behavior)
    "fail" -> throwIO (PublicError (text (get "code" behavior)) (text (get "message" behavior)) (case behavior of Object xs -> KM.lookup "data" xs; _ -> Nothing))
    "wait" -> awaitCancellation ctx >> throwIO (PublicError "cancelled" "handler cancelled" Nothing)
    "hold" -> do
      released <- newEmptyTMVarIO
      detach <- onEvent remote $ \_ event _ -> when (event == text (get "until" behavior)) (atomically (void (tryPutTMVar released ())))
      flip finally detach (atomically (readTMVar released))
      pure (get "value" behavior)
    "panic" -> throwIO (userError "private handler panic")
    "reverse" -> call remote ctx (text (get "method" behavior)) (case behavior of Object xs -> fromMaybe params (KM.lookup "params" xs); _ -> params)
    "emit" -> emit remote ctx (text (get "event" behavior)) (get "data" behavior) >> pure (get "then" behavior)
    _ -> err "invalid" "unknown canned handler"
  let outcome = case result of Right _ -> "ok"; Left (e::SomeException) -> case fromException e of Just pe | errorCode pe == "cancelled" -> "cancelled"; _ | text (get "kind" behavior) == "panic" -> "panic"; _ -> "error"
  put (requestInbox p) (object ["method" .= method, "phase" .= ("ended"::Text), "outcome" .= (outcome::Text)])
  either throwIO pure result

dispatch :: Testee -> Value -> IO Value
dispatch t args = case text (get "op" args) of
  "hello" -> pure (object ["driver" .= (1::Int), "language" .= ("haskell"::Text), "layers" .= (["seam","peer"]::[Text]), "features" .= (["pipe","listen","lazy","propagator"]::[Text])])
  "reset" -> reset t >> pure empty
  "bye" -> reset t >> pure empty
  "conn.listen" -> do
    listener <- WS.listen (num 1048576 (get "limit" args)) []
    key <- mint t (Listen listener Nothing)
    pure (object ["handle" .= key, "url" .= WS.listenerURL listener])
  "conn.accept" -> do
    held <- lookupHeld t args
    case held of
      Listen listener _ -> within args $ do
        conn <- WS.accept listener >>= \c -> wrapped c (get "consume" args == String "lazy")
        key <- mint t (Conn conn)
        pure (object ["handle" .= key])
      _ -> err "invalid" "expected listener"
  "conn.dial" -> within args $ do
    connection <- WS.dial (text (get "url" args)) (num 1048576 (get "limit" args)) []
    conn <- wrapped connection (get "consume" args == String "lazy")
    key <- mint t (Conn conn)
    pure (object ["handle" .= key])
  "conn.pipe" -> do
    (a,b) <- pipe (num 1048576 (get "limit" args))
    ca <- wrapped a (get "consume" args == String "lazy") >>= mint t . Conn
    cb <- wrapped b (get "consume" args == String "lazy") >>= mint t . Conn
    pure (object ["a" .= ca, "b" .= cb])
  "conn.send" -> do
    c <- connOf t args
    frame <- case text (get "kind" args) of
      "text" -> pure (Frame TextFrame (TE.encodeUtf8 (text (get "text" args))))
      "binary" -> either (err "invalid" . T.pack) (pure . Frame BinaryFrame) (Base64.decode (TE.encodeUtf8 (text (get "base64" args))))
      _ -> err "invalid" "expected text or binary"
    within args (sendFrame (native c) frame)
    pure empty
  "conn.receive" -> do
    c <- connOf t args
    frame <- within args (receiveControlled c)
    pure $ case frameKind frame of
      TextFrame -> object ["kind" .= ("text"::Text), "text" .= TE.decodeUtf8With TE.lenientDecode (frameData frame)]
      BinaryFrame -> object ["kind" .= ("binary"::Text), "base64" .= TE.decodeUtf8 (Base64.encode (frameData frame))]
  "conn.close" -> do
    c <- connOf t args
    within args (closeConnection (native c) (num 1000 (get "code" args)) (text (get "reason" args)))
    pure empty
  "conn.abort" -> connOf t args >>= abortConnection . native >> pure empty
  "conn.await_close" -> do
    c <- connOf t args
    result <- within args $ if lazy c then do
      finished <- try (forever (receiveControlled c)) :: IO (Either CloseError ())
      either pure (const (pure (CloseError 1006 ""))) finished
      else atomically (readTMVar (connectionEnded c))
    pure (object ["code" .= closeCode result, "reason" .= closeReason result])
  "peer.listen" -> do
    opts <- options args
    listener <- WS.listen (maxFrameBytes opts) (strings (get "subprotocols" args))
    key <- mint t (Listen listener (Just opts))
    pure (object ["handle" .= key, "url" .= WS.listenerURL listener])
  "peer.accept" -> do
    held <- lookupHeld t args
    case held of
      Listen listener (Just opts) -> within args $ do
        connection <- WS.accept listener
        peer <- newPeer connection Server opts >>= adopt
        key <- mint t (Peer peer)
        pure (object ["handle" .= key, "subprotocol" .= peerSubprotocol (nativePeer peer)])
      _ -> err "invalid" "expected peer listener"
  "peer.dial" -> within args $ do
    opts <- options args
    connection <- WS.dial (text (get "url" args)) (maxFrameBytes opts) (strings (get "subprotocols" args))
    peer <- newPeer connection Client opts >>= adopt
    key <- mint t (Peer peer)
    pure (object ["handle" .= key, "subprotocol" .= peerSubprotocol (nativePeer peer)])
  "peer.over" -> do
    c <- connOf t args
    unless (lazy c) (err "invalid" "peer requires lazy connection")
    opts <- options args
    let role = if text (get "role" args) == "client" then Client else Server
    peer <- newPeer (native c) role opts >>= adopt
    key <- mint t (Peer peer)
    pure (object ["handle" .= key])
  "peer.handle" -> do
    peer <- peerOf t args
    let name = text (get "method" args)
    handle (nativePeer peer) name (handler peer name (get "behavior" args))
    pure empty
  "peer.on_event" -> do
    peer <- peerOf t args
    case text (get "kind" (get "behavior" args)) of
      "block" -> handleEvent (nativePeer peer) (text (get "name" args)) (\ctx _ -> awaitCancellation ctx)
      "panic" -> handleEvent (nativePeer peer) (text (get "name" args)) (\_ _ -> throwIO (userError "event panic"))
      _ -> pure ()
    pure empty
  "peer.call" -> do
    peer <- peerOf t args
    ctx <- context args
    task <- async ((Right <$> call (nativePeer peer) ctx (text (get "method" args)) (get "params" args)) `catch` \(e::SomeException) -> pure (Left (asError e)))
    key <- mint t (Call ctx task)
    pure (object ["handle" .= key])
  "call.await" -> do
    held <- lookupHeld t args
    case held of Call _ task -> within args (wait task) >>= pure . either (\e -> object ["error" .= e]) (\v -> object ["result" .= v]); _ -> err "invalid" "expected call"
  "call.cancel" -> do
    held <- lookupHeld t args
    case held of Call ctx _ -> cancelContext ctx >> pure empty; _ -> err "invalid" "expected call"
  "peer.emit" -> do
    peer <- peerOf t args
    ctx <- context args
    within args (emit (nativePeer peer) ctx (text (get "event" args)) (get "data" args))
    pure empty
  "peer.await_event" -> do
    peer <- peerOf t args
    value <- within args (takeMatching (eventInbox peer) (\v -> get "name" v == get "name" args))
    pure (case value of Object xs -> Object (KM.delete "name" xs); _ -> value)
  "peer.await_request" -> do
    peer <- peerOf t args
    within args (takeMatching (requestInbox peer) (\v -> get "method" v == get "method" args && get "phase" v == get "phase" args))
  "peer.close" -> peerOf t args >>= closePeer . nativePeer >> pure empty
  "peer.await_close" -> do
    peer <- peerOf t args
    reason <- within args (awaitClosed (nativePeer peer))
    pure (object ["code" .= closeCode reason, "clean" .= (closeCode reason == 1000)])
  "peer.identity" -> do
    peer <- peerOf t args
    h <- identityHandler (DeclarationIdentity (text (get "path" args)) (text (get "digest" args)))
    handle (nativePeer peer) "identity.check" h
    pure empty
  "peer.check_identity" -> do
    peer <- peerOf t args
    ctx <- context args
    within args (checkIdentity (nativePeer peer) ctx (DeclarationIdentity (text (get "path" args)) (text (get "digest" args))))
    pure empty
  "peer.recorded_wire_witness" -> within args recordedWireWitness
  op -> err "unsupported" ("unknown op " <> op)

main :: IO ()
main = do
  hSetBinaryMode stdin True
  hSetBinaryMode stdout True
  hSetBuffering stdout NoBuffering
  testee <- Testee <$> newTVarIO 0 <*> newTVarIO M.empty
  let loop = do
        eof <- hIsEOF stdin
        unless eof $ do
          line <- B.hGetLine stdin
          case eitherDecodeStrict' line of
            Left why -> L.hPut stdout (encode (object ["id" .= Null, "error" .= object ["code" .= ("invalid"::Text), "message" .= why]]) <> "\n") >> loop
            Right request -> do
              result <- try (dispatch testee request)
              let answer = object (["id" .= get "id" request] ++ either (\(e::SomeException) -> ["error" .= asError e]) (\value -> ["ok" .= value]) result)
              L.hPut stdout (encode answer <> "\n")
              unless (get "op" request == String "bye") loop
  loop `finally` reset testee
