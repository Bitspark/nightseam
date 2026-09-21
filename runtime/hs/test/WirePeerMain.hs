{-# LANGUAGE OverloadedStrings #-}
{-# LANGUAGE ScopedTypeVariables #-}
module Main (main) where

import Control.Concurrent.STM
import Control.Concurrent (forkIO, threadDelay)
import Control.Exception hiding (assert, handle)
import Control.Monad
import Data.Aeson hiding (Options, defaultOptions)
import qualified Data.Aeson.Key as K
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy as L
import Data.Dynamic (fromDynamic, toDyn)
import qualified Data.Map.Strict as M
import Data.Text (Text)
import qualified Data.Text as T
import Nightseam.Duplex
import Nightseam.Duplex.Wire
import Nightseam.Runtime
import System.Timeout

assert :: String -> Bool -> IO ()
assert label okay = unless okay (fail label)

within :: IO a -> IO a
within action = timeout 2000000 action >>= maybe (fail "test deadline expired") pure

field :: Text -> Value -> Value
field key (Object fields) = maybe Null id (KM.lookup (K.fromText key) fields)
field _ _ = Null

-- The writer can be held independently of receive, which makes queue bounds,
-- cancellation reservations and publication order observable without sleeps.
fixture :: Options -> Bool -> IO (Peer, TQueue Value, Value -> IO (), IO ())
fixture opts held = do
  sent <- newTQueueIO
  incoming <- newTQueueIO
  closed <- newEmptyTMVarIO
  released <- newTVarIO (not held)
  let conn = Connection
        { sendFrame = \frame -> do
            value <- maybe (fail "writer emitted invalid JSON") pure (decodeStrict' (frameData frame))
            atomically (writeTQueue sent value)
            atomically ((readTVar released >>= check) `orElse` (readTMVar closed >>= throwSTM))
        , receiveFrame = atomically ((readTQueue incoming) `orElse` (readTMVar closed >>= throwSTM))
        , closeConnection = \code reason -> atomically (void (tryPutTMVar closed (CloseError code reason)))
        , abortConnection = atomically (void (tryPutTMVar closed (CloseError 1006 "")))
        , connectionSubprotocol = ""
        }
  peer <- newPeer conn Client opts
  pure (peer, sent, \v -> atomically (writeTQueue incoming (Frame TextFrame (L.toStrict (encode v)))), atomically (writeTVar released True))

message :: Text -> Text -> Value -> Maybe ReturnAddress -> Maybe CallContext -> Message
message kind ident payload returning ctx = Message
  (object (["version" .= (1 :: Int), "kind" .= kind] ++
    (if kind == "event" then ["data" .= payload] else ["id" .= ident] ++ if kind == "request" then ["params" .= payload] else [])))
  returning (toDyn <$> ctx)

returningTo :: TQueue Value -> IO ReturnAddress
returningTo replies = newReturnAddress (Wire (\_ m -> atomically (writeTQueue replies (messageFrame m))) (\_ _ -> throwIO NoRoute) (\_ _ -> pure ()))

refused :: IO a -> IO ()
refused action = do
  result <- try (void action) :: IO (Either SomeException ())
  assert "invalid operation is refused" (either (const True) (const False) result)

main :: IO ()
main = do
  failures <- forM [(routed, deadline, declines) | routed <- [False, True], deadline <- [True, False], declines <- [False, True]] $ \(routed, deadline, declines) -> do
    result <- try (incomingBudget routed deadline declines) :: IO (Either SomeException ())
    case result of
      Left err -> putStrLn ("incoming budget " ++ show (routed, deadline, declines) ++ ": " ++ show err) >> pure True
      Right () -> pure False
  assert "incoming cancellation and deadline preserve active work" (not (or failures))
  forM_ [False, True] $ \routed -> replicateM_ 16 (receiverDeadlineRefusal routed)
  requestOrderAndCancellation
  reservedCancellation
  overflowIsAsynchronous
  registrationLifetime
  subscriptionLifetime
  validateReturns
  validateLocalFrames
  cancellationAfterDetach
  boundedExplicitClose
  putStrLn "peer Wire tests passed"

-- A response deadline and actual application completion are separate: the
-- former settles the caller, while only the latter retires admitted work.
incomingBudget :: Bool -> Bool -> Bool -> IO ()
incomingBudget routed deadline declines = do
  let opts = defaultOptions {maxConcurrentHandlers = 1, requestTimeoutMs = if deadline then 75 else 5000}
  (peer, sent, inject, _) <- fixture opts False
  entered <- newEmptyTMVarIO
  cancelled <- newEmptyTMVarIO
  released <- newEmptyTMVarIO
  let body ctx = do
        atomically (putTMVar entered ())
        awaitCancellation ctx
        atomically (putTMVar cancelled ())
        atomically (readTMVar released)
        if declines then throwIO (PublicError "declined" "Application refusal" Nothing) else pure Null
      cleanup = atomically (void (tryPutTMVar released ())) >> closePeer peer
      request :: Text -> Text -> IO ()
      request ident method = inject (object ["version" .= (1 :: Int), "kind" .= ("request" :: Text),
        "id" .= ident, "method" .= method, "params" .= Null])
      next = within (atomically (readTQueue sent))
      refusal ident code answer = field "id" answer == String ident && field "code" (field "error" answer) == String code
  flip finally cleanup $ do
    incomingBody peer routed body
    handle peer "echo" (\_ _ _ -> pure (String "released"))
    request ("s:1" :: Text) (if routed then "4:hold" else "hold")
    within (atomically (readTMVar entered))
    unless deadline $ inject (object ["version" .= (1 :: Int), "kind" .= ("cancel" :: Text), "id" .= ("s:1" :: Text)])
    within (atomically (readTMVar cancelled))
    when deadline $ next >>= assert "receiver deadline answers before application return" . refusal "s:1" "cancelled"
    request ("s:2" :: Text) ("echo" :: Text)
    next >>= assert "cancelled application still owns its handler slot" . refusal "s:2" "busy"
    premature <- timeout 50000 (atomically (readTQueue sent))
    assert "no early or duplicate answer while the body remains active" (premature == Nothing)
    atomically (putTMVar released ())
    unless deadline $ next >>= assert "explicit cancellation preserves an application refusal" . refusal "s:1" (if declines then "declined" else "cancelled")
    let retryEcho n = do
          let ident = "s:" <> T.pack (show n)
          request ident ("echo" :: Text)
          answer <- next
          assert "deadline response is sent only once" (field "id" answer == String ident)
          if refusal ident "busy" answer then threadDelay 1000 >> retryEcho (n + 1)
          else assert "application return restores capacity" (field "result" answer == String "released")
    within (retryEcho (3 :: Int))

incomingBody :: Peer -> Bool -> (CallContext -> IO Value) -> IO ()
incomingBody peer routed body =
  if routed then void $ wireReceive (peerWire peer) ["hold"] (Receiver False
    (\_ incoming -> when (field "kind" (messageFrame incoming) == String "request") $ do
      ctx <- maybe (fail "incoming Wire lost its context") pure (messageContext incoming >>= fromDynamic)
      address <- maybe (fail "incoming Wire lost its return address") pure (messageReturn incoming)
      void $ forkIO $ void (try (do
        outcome <- try (body ctx) :: IO (Either PublicError Value)
        let answer = either (\err -> ["error" .= publicErrorValue err]) (\value -> ["result" .= value]) outcome
        wireSend (returnWire address) [] (Message (object (["version" .= (1 :: Int), "kind" .= ("response" :: Text),
          "id" .= field "id" (messageFrame incoming)] ++ answer)) Nothing Nothing)) :: IO (Either SomeException ())))
    (\_ _ -> pure ()))
  else handle peer "hold" (\ctx _ _ -> body ctx)

-- The body refuses as soon as its receiver deadline wakes it. The deadline
-- response must already be decided, independently of that late refusal.
receiverDeadlineRefusal :: Bool -> IO ()
receiverDeadlineRefusal routed = do
  (peer, sent, inject, _) <- fixture defaultOptions {requestTimeoutMs = 10} False
  flip finally (closePeer peer) $ do
    incomingBody peer routed $ \ctx -> do
      awaitCancellation ctx
      throwIO (PublicError "declined" "Too late" Nothing)
    inject (object ["version" .= (1 :: Int), "kind" .= ("request" :: Text), "id" .= ("s:1" :: Text),
      "method" .= (if routed then "4:hold" else "hold" :: Text), "params" .= Null])
    answer <- within (atomically (readTQueue sent))
    assert "receiver deadline wins over a body refusal after cancellation"
      (field "id" answer == String "s:1" && field "code" (field "error" answer) == String "cancelled")

requestOrderAndCancellation :: IO ()
requestOrderAndCancellation = do
  (peer, sent, inject, _) <- fixture defaultOptions False
  flip finally (closePeer peer) $ do
    replies <- newTQueueIO
    address <- returningTo replies
    ctx <- newCallContext
    let wire = peerWire peer
        sendRequest ident = wireSend wire ["op"] (message "request" ident Null (Just address) (Just ctx))
    refused (wireSend wire ["op"] (message "request" "c:9" Null Nothing Nothing))
    sendRequest "c:1"
    wireSend wire ["op"] (message "event" "" Null Nothing Nothing)
    sendRequest "c:2"
    refused (sendRequest "c:2")
    first <- within (atomically (readTQueue sent))
    second <- within (atomically (readTQueue sent))
    third <- within (atomically (readTQueue sent))
    assert "sequential Wire request/event/request preserves publication order"
      (map (field "kind") [first, second, third] == [String "request", String "event", String "request"])
    wireSend wire [] (message "cancel" "c:1" Null (Just address) Nothing)
    wasCancelled <- atomically (not <$> isEmptyTMVar (contextCancelled ctx))
    assert "withdrawing one Wire request does not cancel shared context" (not wasCancelled)
    cancel <- within (atomically (readTQueue sent))
    assert "cancel retains physical request correlation" (field "id" cancel == field "id" first)
    inject (object ["version" .= (1 :: Int), "kind" .= ("response" :: Text), "id" .= field "id" third, "result" .= ("alive" :: Text)])
    answers <- replicateM 2 (within (atomically (readTQueue replies)))
    assert "a sibling request survives cancellation" (any (\v -> field "id" v == String "c:2" && field "result" v == String "alive") answers)
    sendRequest "c:3"
    refusedRequest <- within (atomically (readTQueue sent))
    inject (object ["version" .= (1 :: Int), "kind" .= ("response" :: Text), "id" .= field "id" refusedRequest,
      "error" .= object ["code" .= ("cancelled" :: Text), "message" .= ("refused" :: Text)]])
    _ <- within (atomically (readTQueue replies))
    wireSend wire ["op"] (message "event" "" Null Nothing Nothing)
    fence <- within (atomically (readTQueue sent))
    assert "a remote public cancelled error does not fabricate a withdrawal" (field "kind" fence == String "event")
    sendRequest "c:4"
    _ <- within (atomically (readTQueue sent))
    closePeer peer
    closedAnswer <- within (atomically (readTQueue replies))
    assert "peer closure settles admitted Wire return" (field "code" (field "error" closedAnswer) == String "disconnected")

reservedCancellation :: IO ()
reservedCancellation = do
  (peer, sent, _, release) <- fixture defaultOptions {queueCapacity = 1, maxPendingRequests = 1} True
  flip finally (closePeer peer) $ do
    replies <- newTQueueIO
    address <- returningTo replies
    let wire = peerWire peer
        request ident = message "request" ident Null (Just address) Nothing
        cancel ident = message "cancel" ident Null (Just address) Nothing
    wireSend wire ["op"] (request "c:1")
    _ <- within (atomically (readTQueue sent))
    wireSend wire ["op"] (message "event" "" Null Nothing Nothing)
    wireSend wire [] (cancel "c:1")
    replicateM_ 100 (wireSend wire [] (cancel "c:1") >> wireSend wire [] (cancel "c:9"))
    answer <- within (atomically (readTQueue replies))
    assert "cancellation settles while data queue is full" (field "code" (field "error" answer) == String "cancelled")
    refused (wireSend wire ["op"] (request "c:2"))
    release
    event <- within (atomically (readTQueue sent))
    control <- within (atomically (readTQueue sent))
    assert "reserved cancellation follows previously admitted data" (field "kind" event == String "event" && field "kind" control == String "cancel")
    wireSend wire ["op"] (request "c:2")
    next <- within (atomically (readTQueue sent))
    assert "duplicate and unknown cancels occupy no extra slots" (field "kind" next == String "request")

overflowIsAsynchronous :: IO ()
overflowIsAsynchronous = do
  (peer, sent, _, _) <- fixture defaultOptions {queueCapacity = 1} True
  flip finally (closePeer peer) $ do
    callback <- newEmptyTMVarIO
    released <- newEmptyTMVarIO
    let wire = peerWire peer
        event = message "event" "" Null Nothing Nothing
    _ <- wireReceive wire ["end"] (Receiver False (\_ _ -> pure ()) (\_ _ -> atomically (putTMVar callback ()) >> atomically (readTMVar released)))
    wireSend wire ["op"] event
    _ <- within (atomically (readTQueue sent))
    wireSend wire ["op"] event
    within (refused (wireSend wire ["op"] event))
    within (atomically (readTMVar callback))
    atomically (putTMVar released ())

registrationLifetime :: IO ()
registrationLifetime = do
  (peer, _, inject, _) <- fixture defaultOptions False
  flip finally (closePeer peer) $ do
    received <- newTQueueIO
    let wire = peerWire peer
        receiver label namespace = Receiver namespace (\_ _ -> atomically (writeTQueue received label)) (\_ _ -> pure ())
        event path = let name = either (error . show) id (encodePath path) in object
          ["version" .= (1 :: Int), "kind" .= ("event" :: Text), "event" .= name, "data" .= Null]
    detach <- wireReceive wire ["a"] (receiver ("old" :: Text) False)
    detach
    _ <- wireReceive wire ["a"] (receiver "exact" False)
    _ <- wireReceive wire ["a"] (receiver "namespace" True)
    detach
    inject (event ["a"])
    inject (event ["a", "b"])
    actual <- replicateM 2 (within (atomically (readTQueue received)))
    assert "stale detach cannot remove replacement; exact wins over namespace" (actual == ["exact", "namespace"])

subscriptionLifetime :: IO ()
subscriptionLifetime = do
  (peer, _, inject, _) <- fixture defaultOptions False
  flip finally (closePeer peer) $ do
    received <- newTQueueIO
    old <- onEvent peer (\_ _ _ -> atomically (writeTQueue received ("old" :: Text)))
    old
    _ <- onEvent peer (\_ _ _ -> atomically (writeTQueue received "new"))
    old
    inject (object ["version" .= (1 :: Int), "kind" .= ("event" :: Text), "event" .= ("tick" :: Text), "data" .= Null])
    actual <- within (atomically (readTQueue received))
    assert "stale unsubscribe cannot remove a later subscriber" (actual == "new")

validateReturns :: IO ()
validateReturns = do
  (peer, sent, inject, _) <- fixture defaultOptions False
  flip finally (closePeer peer) $ do
    received <- newEmptyTMVarIO
    _ <- wireReceive (peerWire peer) ["op"] (Receiver False
      (\_ m -> maybe (fail "no return address") (atomically . putTMVar received) (messageReturn m)) (\_ _ -> pure ()))
    inject (object ["version" .= (1 :: Int), "kind" .= ("request" :: Text), "id" .= ("s:1" :: Text), "method" .= ("2:op" :: Text), "params" .= Null])
    address <- within (atomically (readTMVar received))
    let reply ident = Message (object ["version" .= (1 :: Int), "kind" .= ("response" :: Text), "id" .= ident,
          "error" .= object ["code" .= ("denied" :: Text), "message" .= ("No" :: Text), "data" .= (7 :: Int)]]) Nothing Nothing
        returnTo = returnWire address
    refused (wireSend returnTo ["wrong"] (reply ("s:1" :: Text)))
    refused (wireSend returnTo [] (reply ("s:2" :: Text)))
    refused (wireSend returnTo [] (message "event" "" Null Nothing Nothing))
    wireSend returnTo [] (reply ("s:1" :: Text))
    refused (wireSend returnTo [] (reply ("s:1" :: Text)))
    response <- within (atomically (readTQueue sent))
    assert "Wire return preserves public error data" (field "data" (field "error" response) == Number 7)

validateLocalFrames :: IO ()
validateLocalFrames = do
  (peer, sent, _, _) <- fixture defaultOptions {maxFrameBytes = 400} False
  flip finally (closePeer peer) $ do
    let wire = peerWire peer
    refused (wireSend wire ["op"] (Message (object ["kind" .= ("event" :: Text), "data" .= Null]) Nothing Nothing))
    refused (wireSend wire ["op"] (message "event" "" (String (T.replicate 500 "a")) Nothing Nothing))
    let trace = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01" :: Text
        event = object ["version" .= (1 :: Int), "kind" .= ("event" :: Text), "data" .= Null,
          "traceparent" .= trace, "meta" .= M.singleton ("header" :: Text) ("value" :: Text)]
    wireSend wire ["op"] (Message event Nothing Nothing)
    published <- within (atomically (readTQueue sent))
    assert "Wire retains explicit profile context without private context" (field "traceparent" published == String trace && field "meta" published == field "meta" event)

cancellationAfterDetach :: IO ()
cancellationAfterDetach = do
  (peer, sent, inject, _) <- fixture defaultOptions False
  flip finally (closePeer peer) $ do
    captured <- newTQueueIO
    replacements <- newTQueueIO
    let wire = peerWire peer
    detach <- wireReceive wire ["op"] (Receiver False (\_ m -> atomically (writeTQueue captured m)) (\_ _ -> pure ()))
    inject (object ["version" .= (1 :: Int), "kind" .= ("request" :: Text), "id" .= ("s:1" :: Text), "method" .= ("2:op" :: Text), "params" .= Null])
    request <- within (atomically (readTQueue captured))
    assert "request reaches original receiver" (field "kind" (messageFrame request) == String "request")
    detach
    _ <- wireReceive wire ["op"] (Receiver False (\_ m -> atomically (writeTQueue replacements (messageFrame m))) (\_ _ -> pure ()))
    inject (object ["version" .= (1 :: Int), "kind" .= ("cancel" :: Text), "id" .= ("s:1" :: Text)])
    cancelled <- within (atomically (readTQueue captured))
    assert "cancel retains receiver captured before detach" (field "kind" (messageFrame cancelled) == String "cancel")
    premature <- timeout 50000 (atomically (readTQueue sent))
    assert "detaching does not finish the captured application" (premature == Nothing)
    address <- maybe (fail "captured request has no return address") pure (messageReturn request)
    wireSend (returnWire address) [] (Message (object ["version" .= (1 :: Int), "kind" .= ("response" :: Text),
      "id" .= ("s:1" :: Text), "result" .= Null]) Nothing Nothing)
    response <- within (atomically (readTQueue sent))
    assert "captured cancel completes once" (field "code" (field "error" response) == String "cancelled")
    empty <- atomically (isEmptyTQueue replacements)
    assert "replacement receives no old cancellation" empty

boundedExplicitClose :: IO ()
boundedExplicitClose = do
  (peer, _, inject, _) <- fixture defaultOptions {writeTimeoutMs = 500} False
  requestStarted <- newEmptyTMVarIO
  eventStarted <- newEmptyTMVarIO
  requestEnded <- newEmptyTMVarIO
  eventEnded <- newEmptyTMVarIO
  never <- newEmptyTMVarIO
  handle peer "ignore" $ \_ _ _ ->
    (atomically (putTMVar requestStarted ()) >> atomically (readTMVar never))
      `finally` atomically (putTMVar requestEnded ())
  handleEvent peer "ignore" $ \_ _ ->
    (atomically (putTMVar eventStarted ()) >> atomically (readTMVar never) >> pure ())
      `finally` atomically (putTMVar eventEnded ())
  inject (object ["version" .= (1 :: Int), "kind" .= ("request" :: Text), "id" .= ("s:1" :: Text), "method" .= ("ignore" :: Text), "params" .= Null])
  inject (object ["version" .= (1 :: Int), "kind" .= ("event" :: Text), "event" .= ("ignore" :: Text), "data" .= Null])
  within (atomically (readTMVar requestStarted >> readTMVar eventStarted))
  within (closePeer peer)
  within (atomically (readTMVar requestEnded >> readTMVar eventEnded))
