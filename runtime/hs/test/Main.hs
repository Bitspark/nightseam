{-# LANGUAGE OverloadedStrings #-}
module Main where
import Control.Concurrent.Async
import Control.Concurrent (threadDelay)
import Control.Concurrent.STM
import Control.Exception (try)
import Control.Monad
import Data.Aeson hiding (Options, defaultOptions)
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy as L
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import Data.Foldable (toList)
import qualified Data.Map.Strict as M
import Nightseam.Duplex
import Nightseam.Runtime
import System.Timeout

main :: IO ()
main = do
  serials
  publicationBudget
  (a,b) <- pipe 1048576
  client <- newPeer a Client defaultOptions
  server <- newPeer b Server defaultOptions
  handle server "echo" (\_ _ value -> pure value)
  ctx <- newCallContext
  result <- call client ctx "echo" (object ["answer" .= (42 :: Int)])
  unless (result == object ["answer" .= (42 :: Int)]) (error "round trip")
  -- Incoming metadata remains observable without becoming outbound authority.
  let credential = M.singleton "authorization" "private"
      explicit = M.singleton "chosen" "outbound"
  handle client "inspect-meta" (\incoming _ _ -> pure (toJSON (contextReceivedMeta incoming)))
  handle server "reverse-meta" $ \incoming remote _ -> do
    unless (contextReceivedMeta incoming == credential) (error "incoming metadata missing")
    call remote incoming "inspect-meta" Null
  reversed <- call client ctx { contextMeta = credential } "reverse-meta" Null
  unless (reversed == object []) (error "reverse call implicitly copied received metadata")
  handle server "explicit-meta" $ \incoming remote _ ->
    call remote incoming { contextMeta = explicit } "inspect-meta" Null
  selected <- call client ctx { contextMeta = credential } "explicit-meta" Null
  unless (selected == toJSON explicit) (error "explicit reverse metadata missing")
  events <- newTQueueIO
  detach <- onEvent client $ \incoming _ _ -> atomically (writeTQueue events (contextReceivedMeta incoming))
  handle server "emit-meta" $ \incoming remote _ -> do
    emit remote incoming "ordinary" Null
    emit remote incoming { contextMeta = explicit } "selected" Null
    pure Null
  _ <- call client ctx { contextMeta = credential } "emit-meta" Null
  ordinaryEvent <- timeout 1000000 (atomically (readTQueue events))
  selectedEvent <- timeout 1000000 (atomically (readTQueue events))
  unless (ordinaryEvent == Just M.empty && selectedEvent == Just explicit)
    (error "request metadata leaked to an event or explicit event metadata was lost")
  handleEvent server "reflect-meta" $ \incoming value -> do
    unless (contextReceivedMeta incoming == credential) (error "incoming event metadata missing")
    emit server incoming "reflected" value
  emit client ctx { contextMeta = credential } "reflect-meta" Null
  reflectedEvent <- timeout 1000000 (atomically (readTQueue events))
  unless (reflectedEvent == Just M.empty) (error "event metadata implicitly propagated")
  detach
  began <- newEmptyTMVarIO
  handle server "wait" $ \incoming _ _ -> do
    atomically (putTMVar began ())
    awaitCancellation incoming
    pure Null
  c <- newCallContext
  running <- async (call client c "wait" Null)
  atomically (takeTMVar began)
  cancelContext c
  outcome <- waitCatch running
  unless (either (const True) (const False) outcome) (error "cancel settles call")
  again <- call client ctx "echo" (String "alive")
  unless (again == String "alive") (error "cancel preserves connection")
  closePeer client
  closePeer server
  -- Cancellation withdraws a request; a handler ignoring it retains its slot.
  (c2,s2) <- pipe 1048576
  caller <- newPeer c2 Client defaultOptions
  callee <- newPeer s2 Server defaultOptions { maxConcurrentHandlers = 1 }
  retained <- newEmptyTMVarIO
  release <- newEmptyTMVarIO
  handle callee "hold" $ \_ _ _ -> atomically (putTMVar retained ()) >> atomically (takeTMVar release) >> pure Null
  handle callee "echo" (\_ _ value -> pure value)
  heldCtx <- newCallContext
  held <- async (call caller heldCtx "hold" Null)
  atomically (takeTMVar retained)
  cancelContext heldCtx
  _ <- waitCatch held
  checkCtx <- newCallContext
  busy <- try (call caller checkCtx "echo" Null) :: IO (Either PublicError Value)
  unless (either ((== "busy") . errorCode) (const False) busy) (error "cancel freed an active handler slot")
  atomically (putTMVar release ())
  let retryEcho = do
        answer <- try (call caller checkCtx "echo" (String "released"))
        case answer of
          Right value -> pure value
          Left failure | errorCode failure == "busy" -> threadDelay 1000 >> retryEcho
          Left failure -> error (show failure)
  released <- timeout 1000000 retryEcho
  unless (released == Just (String "released")) (error "handler return failed to release its slot")
  closePeer caller
  closePeer callee
  putStrLn "peer tests passed"


serials :: IO ()
serials = do
  table <- eitherDecodeFileStrict "../../conformance/tables/serials.json" >>= either error pure
  let field key (Object fields) = maybe Null id (KM.lookup key fields)
      field _ _ = Null
      text (String value) = value
      text _ = error "expected serial-table text"
      rows = case field "rows" table of Array values -> toList values; _ -> error "serial rows missing"
  forM_ rows $ \row -> do
    (raw, transport) <- pipe 1048576
    peer <- newPeer transport Server defaultOptions
    handle peer "echo" (\_ _ value -> pure value)
    let before = text (field "before" row)
    sendFrame raw (Frame TextFrame (TE.encodeUtf8 before))
    when (field "kind" (maybe Null id (decodeStrict (TE.encodeUtf8 before))) == String "request") $
      void (receiveFrame raw)
    sendFrame raw (Frame TextFrame (TE.encodeUtf8 (text (field "frame" row))))
    if field "valid" row == Bool True then do
      reply <- timeout 1000000 (receiveFrame raw)
      when (reply == Nothing) (error "valid serial did not receive a response")
    else do
      ended <- timeout 1000000 (awaitClosed peer)
      unless (maybe False ((==4011) . closeCode) ended) (error "non-increasing serial admitted")
    closePeer peer
  replicateM_ 8 $ do
    (raw, transport) <- pipe 1048576
    peer <- newPeer transport Client defaultOptions { queueCapacity = 1 }
    ctx <- newCallContext
    calls <- replicateM 24 (async (call peer ctx "probe" Null))
    ids <- replicateM 24 $ do
      frame <- timeout 1000000 (receiveFrame raw) >>= maybe (error "missing published request") pure
      let value = maybe Null id (decode (L.fromStrict (frameData frame)))
      pure (read (T.unpack (T.drop 2 (text (field "id" value)))) :: Integer)
    closePeer peer
    mapM_ waitCatch calls
    unless (and (zipWith (<) ids (drop 1 ids))) (error "request publication inverted serials")


publicationBudget :: IO ()
publicationBudget = do
  sending <- newEmptyTMVarIO
  release <- newEmptyTMVarIO
  let carrier = Connection
        { sendFrame = \_ -> atomically (void (tryPutTMVar sending ())) >> atomically (readTMVar release)
        , receiveFrame = atomically retry
        , closeConnection = \_ _ -> atomically (void (tryPutTMVar release ()))
        , abortConnection = atomically (void (tryPutTMVar release ()))
        , connectionSubprotocol = ""
        }
  peer <- newPeer carrier Client defaultOptions { maxPendingRequests = 1, queueCapacity = 1, writeTimeoutMs = 1000 }
  ctx <- newCallContext
  emit peer ctx "writing" Null
  atomically (readTMVar sending)
  emit peer ctx "queued" Null
  first <- async (call peer ctx "waiting" Null)
  threadDelay 20000
  second <- timeout 200000 (try (call peer ctx "over-budget" Null) :: IO (Either PublicError Value))
  atomically (void (tryPutTMVar release ()))
  closePeer peer
  _ <- waitCatch first
  unless (case second of Just (Left err) -> errorCode err == "busy"; _ -> False)
    (error "publication waiters escaped the pending budget")
