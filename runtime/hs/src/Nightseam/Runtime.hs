{-# LANGUAGE DeriveDataTypeable #-}
{-# LANGUAGE OverloadedStrings #-}
{-# LANGUAGE ScopedTypeVariables #-}
module Nightseam.Runtime
  ( Peer, Role(..), Options(..), defaultOptions, CallContext(..), newCallContext
  , cancelContext, awaitCancellation, PublicError(..), Handler, newPeer, call
  , emit, handle, handleEvent, onEvent, closePeer, awaitClosed, peerWire
  , peerSubprotocol, publicErrorValue
  ) where

import Control.Concurrent
import Control.Concurrent.STM
import Control.Exception hiding (handle, Handler)
import Control.Monad
import Data.Aeson hiding (Options, defaultOptions)
import Data.Aeson.Types (Pair)
import qualified Data.Aeson.Key as K
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString as B
import qualified Data.ByteString.Lazy as L
import Data.Dynamic
import Data.List (sortOn)
import qualified Data.Map.Strict as M
import Data.Maybe
import qualified Data.Sequence as S
import Data.Text (Text)
import qualified Data.Text as T
import Data.Word
import Data.Unique (Unique, newUnique)
import Numeric (showHex)
import Nightseam.Duplex
import Nightseam.Duplex.Wire
import Nightseam.Runtime.Validate (decodeFrame)
import System.Random (randomIO)
import System.Timeout

data Role = Client | Server deriving (Eq, Show)
data Options = Options
  { maxFrameBytes :: Int, maxPendingRequests :: Int, maxConcurrentHandlers :: Int
  , queueCapacity :: Int, requestTimeoutMs :: Int, writeTimeoutMs :: Int
  } deriving (Eq, Show)
defaultOptions :: Options
defaultOptions = Options 1048576 128 64 128 30000 10000

data PublicError = PublicError
  { errorCode :: Text, errorMessage :: Text, errorData :: Maybe Value }
  deriving (Eq, Show, Typeable)
instance Exception PublicError

publicErrorValue :: PublicError -> Value
publicErrorValue e = object (["code" .= errorCode e, "message" .= errorMessage e] ++ maybe [] (\d -> ["data" .= d]) (errorData e))

failure :: Text -> Text -> PublicError
failure code message = PublicError code message Nothing

data CallContext = CallContext
  { contextCancelled :: TMVar (), contextTraceparent :: Text, contextTracestate :: Text
  , contextMeta :: M.Map Text Text, contextReceivedMeta :: M.Map Text Text
  , contextTimeoutMs :: Int
  } deriving Typeable

newCallContext :: IO CallContext
newCallContext = CallContext <$> newEmptyTMVarIO <*> pure "" <*> pure "" <*> pure M.empty <*> pure M.empty <*> pure 0
cancelContext :: CallContext -> IO ()
cancelContext ctx = atomically (void (tryPutTMVar (contextCancelled ctx) ()))
awaitCancellation :: CallContext -> IO ()
awaitCancellation ctx = atomically (readTMVar (contextCancelled ctx))
type Handler = CallContext -> Peer -> Value -> IO Value

data PendingCall = PendingCall
  { pendingResult :: TMVar (Either PublicError Value)
  , pendingCancelled :: TVar Bool, pendingCancelQueued :: TVar Bool
  , pendingCompleted :: TVar Bool
  }

-- A request reserves exactly one control entry until its queued cancellation
-- drains. Data capacity never includes those reservations.
data Outgoing = Outgoing
  { outgoingFrames :: S.Seq (Value, Maybe (Text, PendingCall))
  , outgoingData :: Int
  }

data WireRegistration = WireRegistration Unique Receiver
data WireCall = WireCall ReturnAddress Text Text PendingCall

data Peer = Peer
  { pConnection :: Connection, pRole :: Role, pOptions :: Options
  , pOutgoing :: TVar Outgoing, pEvents :: TBQueue Value
  , pPending :: TVar (M.Map Text PendingCall)
  , pIncoming :: TVar (M.Map Text CallContext), pCounter :: TVar Integer
  , pHandlers :: TVar (M.Map Text Handler)
  , pEventHandlers :: TVar (M.Map Text (CallContext -> Value -> IO ()))
  , pSubscribers :: TVar (M.Map Int (Unique, CallContext -> Text -> Value -> IO ()))
  , pReceivers :: TVar (M.Map (Path, Bool) WireRegistration)
  , pClosed :: TMVar CloseError, pThreads :: TVar [ThreadId]
  , pCleanupThreads :: TVar [ThreadId], pTransportClosed :: TMVar ()
  , pWireCalls :: TVar [WireCall]
  , pEventContext :: TVar (Maybe CallContext)
  }

get :: Text -> Value -> Value
get key (Object fields) = fromMaybe Null (KM.lookup (K.fromText key) fields)
get _ _ = Null
str :: Value -> Text
str (String value) = value
str _ = ""
members :: Value -> [Pair]
members (Object object') = KM.toList object'
members _ = []

newPeer :: Connection -> Role -> Options -> IO Peer
newPeer connection role opts = do
  when (any (<=0) [maxFrameBytes opts, maxPendingRequests opts, maxConcurrentHandlers opts, queueCapacity opts, requestTimeoutMs opts, writeTimeoutMs opts]) $
    throwIO (failure "invalid" "peer bounds must be positive")
  peer <- Peer connection role opts
    <$> newTVarIO (Outgoing S.empty 0)
    <*> newTBQueueIO (fromIntegral (queueCapacity opts))
    <*> newTVarIO M.empty <*> newTVarIO M.empty <*> newTVarIO 0
    <*> newTVarIO M.empty <*> newTVarIO M.empty <*> newTVarIO M.empty
    <*> newTVarIO M.empty <*> newEmptyTMVarIO <*> newTVarIO [] <*> newTVarIO [] <*> newEmptyTMVarIO
    <*> newTVarIO [] <*> newTVarIO Nothing
  mapM_ (spawn peer . guardLoop peer) [readLoop peer, writeLoop peer, eventLoop peer]
  pure peer

spawn :: Peer -> IO () -> IO ()
spawn = spawnIn . pThreads

spawnCleanup :: Peer -> IO () -> IO ()
spawnCleanup = spawnIn . pCleanupThreads

spawnIn :: TVar [ThreadId] -> IO () -> IO ()
spawnIn threads action = mask_ $ do
  started <- newEmptyTMVarIO
  tid <- forkIOWithUnmask $ \unmask -> do
    self <- myThreadId
    atomically (readTMVar started)
    unmask action `finally` atomically (modifyTVar' threads (filter (/= self)))
  atomically $ do
    modifyTVar' threads (tid:)
    putTMVar started ()

guardLoop :: Peer -> IO () -> IO ()
guardLoop peer action = action `catch` \(err :: SomeException) -> case fromException err of
  Just ThreadKilled -> pure ()
  _ -> finish peer (fromMaybe (CloseError 1006 "") (fromException err))

finish :: Peer -> CloseError -> IO ()
finish peer reason = mask_ $ do
  ending <- atomically $ do
    won <- tryPutTMVar (pClosed peer) reason
    if not won then pure Nothing else do
      pending <- readTVar (pPending peer)
      forM_ (M.elems pending) $ \out -> do
        writeTVar (pendingCancelQueued out) False
        void (tryPutTMVar (pendingResult out) (Left (failure "disconnected" "peer closed")))
      writeTVar (pPending peer) M.empty
      writeTVar (pWireCalls peer) []
      writeTVar (pOutgoing peer) (Outgoing S.empty 0)
      incoming <- readTVar (pIncoming peer)
      forM_ (M.elems incoming) (\ctx -> void (tryPutTMVar (contextCancelled ctx) ()))
      eventContext <- readTVar (pEventContext peer)
      forM_ eventContext (\ctx -> void (tryPutTMVar (contextCancelled ctx) ()))
      receivers <- readTVar (pReceivers peer)
      writeTVar (pReceivers peer) M.empty
      pure (Just (M.elems receivers))
  forM_ ending $ \receivers -> do
    -- Closing a carrier and notifying application callbacks never runs on a
    -- Wire sender's stack. Each callback is independent of transport shutdown.
    spawnCleanup peer $ flip finally (atomically (void (tryPutTMVar (pTransportClosed peer) ()))) $ do
      let connection = pConnection peer
      result <- timeout (writeTimeoutMs (pOptions peer) * 1000)
        (closeConnection connection (closeCode reason) (closeReason reason))
        `catch` \(_ :: SomeException) -> pure Nothing
      when (isNothing result) (abortConnection connection `catch` \(_ :: SomeException) -> pure ())
    forM_ receivers $ \(WireRegistration _ receiver) -> spawnCleanup peer $
      receiverClosed receiver (closeCode reason) (closeReason reason) `catch` \(_ :: SomeException) -> pure ()

closePeer :: Peer -> IO ()
closePeer peer = do
  finish peer (CloseError 1000 "")
  self <- myThreadId
  let others threads = filter (/= self) <$> readTVar threads
      stop threads = readTVarIO threads >>= mapM_ (\tid -> when (tid /= self) (killThread tid))
      await threads = atomically (others threads >>= check . null)
  completed <- timeout (writeTimeoutMs (pOptions peer) * 1000) $ do
    atomically (readTMVar (pTransportClosed peer))
    stop (pThreads peer)
    await (pThreads peer)
    -- Completion tasks own return capabilities and close notifications; allow
    -- them to run before terminating callbacks that ignore their own shutdown.
    await (pCleanupThreads peer)
  when (isNothing completed) $ do
    remaining <- atomically ((++) <$> others (pThreads peer) <*> others (pCleanupThreads peer))
    forM_ remaining (\tid -> void (forkIO (killThread tid)))

awaitClosed :: Peer -> IO CloseError
awaitClosed = atomically . readTMVar . pClosed
peerSubprotocol :: Peer -> Text
peerSubprotocol = connectionSubprotocol . pConnection

openSTM :: Peer -> STM ()
openSTM peer = tryReadTMVar (pClosed peer) >>= maybe (pure ()) (const (throwSTM (failure "disconnected" "peer closed")))

bounded :: Peer -> IO a -> IO a
bounded peer action = do
  answer <- timeout (writeTimeoutMs (pOptions peer) * 1000) action
  case answer of
    Just value -> pure value
    Nothing -> do
      finish peer (CloseError 1008 "write deadline")
      throwIO (failure "disconnected" "write deadline")

enqueue :: Peer -> Value -> IO ()
enqueue peer frame = do
  when (L.length (encode frame) > fromIntegral (maxFrameBytes (pOptions peer))) $
    throwIO (failure "frame_too_large" "frame exceeds configured limit")
  bounded peer $ atomically $ do
    openSTM peer
    outgoing <- readTVar (pOutgoing peer)
    check (outgoingData outgoing < queueCapacity (pOptions peer))
    writeTVar (pOutgoing peer) outgoing
      {outgoingFrames = outgoingFrames outgoing S.|> (frame, Nothing), outgoingData = outgoingData outgoing + 1}

writeLoop :: Peer -> IO ()
writeLoop peer = forever $ do
  frame <- atomically $ do
    openSTM peer
    outgoing <- readTVar (pOutgoing peer)
    case S.viewl (outgoingFrames outgoing) of
      S.EmptyL -> retry
      (value, control) S.:< rest -> do
        writeTVar (pOutgoing peer) outgoing
          {outgoingFrames = rest, outgoingData = outgoingData outgoing - if isNothing control then 1 else 0}
        forM_ control $ \(ident, pending) -> do
          writeTVar (pendingCancelQueued pending) False
          retirePending peer ident pending
        pure value
  bounded peer (sendFrame (pConnection peer) (Frame TextFrame (L.toStrict (encode frame))))

readLoop :: Peer -> IO ()
readLoop peer = forever $ do
  frame <- receiveFrame (pConnection peer)
  when (frameKind frame /= TextFrame) (finish peer (CloseError 4011 "text frames required") >> throwIO (CloseError 4011 "text frames required"))
  when (B.length (frameData frame) > maxFrameBytes (pOptions peer)) (finish peer (CloseError 1009 "frame too large") >> throwIO (CloseError 1009 "frame too large"))
  value <- case decodeFrame (if pRole peer == Client then "client" else "server") (frameData frame) of
    Left reason -> finish peer (CloseError 4011 reason) >> throwIO (CloseError 4011 reason)
    Right value -> pure value
  let ident = str (get "id" value)
  case str (get "kind" value) of
    "request" -> incomingRequest peer value
    "response" -> atomically $ do
      pending <- readTVar (pPending peer)
      forM_ (M.lookup ident pending) $ \out -> void (tryPutTMVar (pendingResult out) (response value))
    "cancel" -> atomically $ do
      incoming <- readTVar (pIncoming peer)
      forM_ (M.lookup ident incoming) $ \ctx -> void (tryPutTMVar (contextCancelled ctx) ())
    "event" -> bounded peer (atomically (openSTM peer >> writeTBQueue (pEvents peer) value))
    _ -> pure ()
  where
    response value = case get "error" value of
      Object e -> Left (PublicError (str (get "code" (Object e))) (str (get "message" (Object e))) (KM.lookup "data" e))
      _ -> Right (get "result" value)

traceFields :: CallContext -> [Pair]
traceFields ctx = (if T.null (contextTraceparent ctx) then [] else ["traceparent" .= contextTraceparent ctx])
  ++ (if T.null (contextTracestate ctx) then [] else ["tracestate" .= contextTracestate ctx])

metaFields :: CallContext -> [Pair]
metaFields ctx = let meta = M.filterWithKey (\key _ -> not ("nightseam." `T.isPrefixOf` key)) (contextMeta ctx)
                in if M.null meta then [] else ["meta" .= meta]

childTrace :: CallContext -> IO CallContext
childTrace ctx = do
  spanId <- randomHex
  let pieces = T.splitOn "-" (contextTraceparent ctx)
  parent <- case pieces of
    [version, traceId, _, flags] -> pure (T.intercalate "-" [version, traceId, spanId, flags])
    _ -> do a <- randomHex; b <- randomHex; pure ("00-" <> a <> b <> "-" <> spanId <> "-01")
  pure ctx { contextTraceparent = parent }
  where
    randomHex = do
      value <- randomIO :: IO Word64
      pure (T.justifyRight 16 '0' (T.pack (showHex (max 1 value) "")))

fromFrame :: Value -> IO CallContext
fromFrame frame = do
  ctx <- newCallContext
  let meta = case get "meta" frame of Object xs -> M.fromList [(K.toText k,t) | (k,String t) <- KM.toList xs]; _ -> M.empty
  pure ctx { contextTraceparent = str (get "traceparent" frame), contextTracestate = str (get "tracestate" frame), contextReceivedMeta = meta }

call :: Peer -> CallContext -> Text -> Value -> IO Value
call peer original method params = mask $ \restore -> do
  when (T.null method) (throwIO (failure "invalid" "method is empty"))
  ctx <- childTrace original
  (ident, pending) <- atomically (reserveCall peer original Nothing)
  let frame = object (["version" .= (1::Int), "kind" .= ("request"::Text), "id" .= ident, "method" .= method, "params" .= params] ++ traceFields ctx ++ metaFields ctx)
  restore (enqueue peer frame) `onException` completePending peer ident pending
  restore (awaitPending peer original ident pending frame)

reserveCall :: Peer -> CallContext -> Maybe (ReturnAddress, Text) -> STM (Text, PendingCall)
reserveCall peer ctx local = do
  openSTM peer
  cancelled <- not <$> isEmptyTMVar (contextCancelled ctx)
  when cancelled (throwSTM (failure "cancelled" "caller cancelled"))
  pending <- readTVar (pPending peer)
  when (M.size pending >= maxPendingRequests (pOptions peer)) (throwSTM (failure "busy" "too many outstanding requests"))
  calls <- readTVar (pWireCalls peer)
  forM_ local $ \(address, localID) ->
    when (any (\(WireCall a i _ _) -> a == address && i == localID) calls)
      (throwSTM (failure "invalid_message" "duplicate active Wire request identifier"))
  next <- (+1) <$> readTVar (pCounter peer)
  writeTVar (pCounter peer) next
  let ident = (if pRole peer == Client then "c:" else "s:") <> T.pack (show next)
  entry <- PendingCall <$> newEmptyTMVar <*> newTVar False <*> newTVar False <*> newTVar False
  writeTVar (pPending peer) (M.insert ident entry pending)
  forM_ local $ \(address, localID) -> writeTVar (pWireCalls peer) (WireCall address localID ident entry : calls)
  pure (ident, entry)

retirePending :: Peer -> Text -> PendingCall -> STM ()
retirePending peer ident pending = do
  completed <- readTVar (pendingCompleted pending)
  queued <- readTVar (pendingCancelQueued pending)
  when (completed && not queued) $ do
    modifyTVar' (pPending peer) (M.delete ident)
    modifyTVar' (pWireCalls peer) (filter (\(WireCall _ _ physical _) -> physical /= ident))

completePending :: Peer -> Text -> PendingCall -> IO ()
completePending peer ident pending = atomically $ do
  writeTVar (pendingCompleted pending) True
  retirePending peer ident pending

withdrawPending :: Peer -> Text -> PendingCall -> Value -> STM ()
withdrawPending peer ident pending request = do
  closed <- not <$> isEmptyTMVar (pClosed peer)
  completed <- readTVar (pendingCompleted pending)
  cancelled <- readTVar (pendingCancelled pending)
  unless (closed || completed || cancelled) $ do
    writeTVar (pendingCancelled pending) True
    writeTVar (pendingCancelQueued pending) True
    outgoing <- readTVar (pOutgoing peer)
    let frame = object (["version" .= (1 :: Int), "kind" .= ("cancel" :: Text), "id" .= ident]
          ++ [(key, value) | key <- ["traceparent", "tracestate"], Just value <- [lookup key (members request)]])
    writeTVar (pOutgoing peer) outgoing {outgoingFrames = outgoingFrames outgoing S.|> (frame, Just (ident, pending))}

awaitPending :: Peer -> CallContext -> Text -> PendingCall -> Value -> IO Value
awaitPending peer ctx ident pending request = mask $ \restore ->
  flip finally (completePending peer ident pending) $ do
    let withdraw = atomically (withdrawPending peer ident pending request)
        deadline = if contextTimeoutMs ctx > 0 then contextTimeoutMs ctx else requestTimeoutMs (pOptions peer)
        cancelled = (readTMVar (contextCancelled ctx) >> pure ()) `orElse` (readTVar (pendingCancelled pending) >>= check)
    answer <- restore (timeout (deadline * 1000) (atomically $
      (Just <$> readTMVar (pendingResult pending)) `orElse` (cancelled >> pure Nothing))) `onException` withdraw
    case answer of
      Nothing -> withdraw >> throwIO (failure "request_timeout" "request deadline")
      Just Nothing -> withdraw >> throwIO (failure "cancelled" "caller cancelled")
      Just (Just (Left err)) -> throwIO err
      Just (Just (Right value)) -> pure value

emit :: Peer -> CallContext -> Text -> Value -> IO ()
emit peer original event payload = do
  ctx <- childTrace original
  enqueue peer (object (["version" .= (1::Int), "kind" .= ("event"::Text), "event" .= event, "data" .= payload] ++ traceFields ctx ++ metaFields ctx))

handle :: Peer -> Text -> Handler -> IO ()
handle peer name handler = atomically $ do
  openSTM peer
  when (T.null name) (throwSTM (failure "invalid" "empty method"))
  handlers <- readTVar (pHandlers peer)
  receivers <- readTVar (pReceivers peer)
  let wireExists = either (const False) (\path -> M.member (path, False) receivers) (decodePath name)
  when (M.member name handlers || wireExists) (throwSTM (failure "invalid" "duplicate method"))
  writeTVar (pHandlers peer) (M.insert name handler handlers)

handleEvent :: Peer -> Text -> (CallContext -> Value -> IO ()) -> IO ()
handleEvent peer name handler = atomically $ do
  openSTM peer
  handlers <- readTVar (pEventHandlers peer)
  receivers <- readTVar (pReceivers peer)
  let wireExists = either (const False) (\path -> M.member (path, False) receivers) (decodePath name)
  when (M.member name handlers || wireExists) (throwSTM (failure "invalid" "duplicate event"))
  writeTVar (pEventHandlers peer) (M.insert name handler handlers)

onEvent :: Peer -> (CallContext -> Text -> Value -> IO ()) -> IO (IO ())
onEvent peer receiver = do
  generation <- newUnique
  atomically $ do
    openSTM peer
    receivers <- readTVar (pSubscribers peer)
    let key = maybe 1 ((+1) . fst) (M.lookupMax receivers)
    writeTVar (pSubscribers peer) (M.insert key (generation, receiver) receivers)
    pure $ atomically $ modifyTVar' (pSubscribers peer) $ \current ->
      case M.lookup key current of
        Just (held, _) | held == generation -> M.delete key current
        _ -> current

matchReceiver :: Peer -> Text -> IO (Maybe (Path, Receiver))
matchReceiver peer method = case decodePath method of
  Left _ -> pure Nothing
  Right path -> do
    receivers <- readTVarIO (pReceivers peer)
    pure $ case M.lookup (path, False) receivers of
      Just (WireRegistration _ receiver) -> Just (path, receiver)
      Nothing -> listToMaybe [(path, r) | ((prefix, namespace), WireRegistration _ r) <- reverse (sortOn (length . fst . fst) (M.toList receivers)), namespace, prefix == take (length prefix) path]

incomingRequest :: Peer -> Value -> IO ()
incomingRequest peer frame = do
  ctx <- fromFrame frame
  let ident = str (get "id" frame)
      method = str (get "method" frame)
      respond answer = enqueue peer (object (["version" .= (1::Int), "kind" .= ("response"::Text), "id" .= ident] ++ answer ++ traceFields ctx))
  admitted <- atomically $ do
    openSTM peer
    incoming <- readTVar (pIncoming peer)
    if M.member ident incoming then throwSTM (CloseError 4011 "duplicate open request")
    else if M.size incoming >= maxConcurrentHandlers (pOptions peer) then pure False
    else writeTVar (pIncoming peer) (M.insert ident ctx incoming) >> pure True
  if not admitted then respond ["error" .= publicErrorValue (failure "busy" "too many active handlers")]
  else spawn peer $ guardLoop peer $ flip finally (atomically (modifyTVar' (pIncoming peer) (M.delete ident))) $ do
    timer <- forkIO (threadDelay (requestTimeoutMs (pOptions peer) * 1000) >> cancelContext ctx)
    outcome <- try $ flip finally (killThread timer) $ do
      handlers <- readTVarIO (pHandlers peer)
      case M.lookup method handlers of
        Just handler -> handler ctx peer (get "params" frame)
        Nothing -> do
          target <- matchReceiver peer method
          case target of
            Nothing -> throwIO (failure "method_not_found" "Unknown method")
            Just (path, receiver) -> do
              result <- newEmptyTMVarIO
              ended <- newTVarIO False
              address <- newReturnAddress Wire
                { wireSend = \returnPath answer -> do
                    when (not (null returnPath) || str (get "kind" (messageFrame answer)) /= "response" || str (get "id" (messageFrame answer)) /= ident)
                      (throwIO (failure "invalid_message" "invalid Wire response"))
                    _ <- validateWireFrame peer [] (messageFrame answer)
                    atomically $ do
                      closed <- readTVar ended
                      when closed (throwSTM WireClosed)
                      accepted <- tryPutTMVar result (messageFrame answer)
                      unless accepted (throwSTM (failure "invalid_message" "duplicate Wire response"))
                , wireReceive = \_ _ -> throwIO NoRoute, wireClose = \_ _ -> atomically (writeTVar ended True) }
              flip finally (atomically (writeTVar ended True)) $ do
                let localFrame = Object (KM.delete "method" (case frame of Object xs -> xs; _ -> KM.empty))
                receiverMessage receiver path (Message localFrame (Just address) (Just (toDyn ctx)))
                answer <- atomically ((Just <$> readTMVar result) `orElse` (readTMVar (contextCancelled ctx) >> pure Nothing)
                  `orElse` (readTVar ended >>= check >> throwSTM WireClosed))
                  >>= maybe (do
                    let cancelFrame = object (["version" .= (1 :: Int), "kind" .= ("cancel" :: Text), "id" .= ident] ++ traceFields ctx)
                    receiverMessage receiver path (Message cancelFrame (Just address) (Just (toDyn ctx)))
                    throwIO (failure "cancelled" "cancelled")) pure
                case get "error" answer of
                  Null -> pure (get "result" answer)
                  e -> throwIO (PublicError (str (get "code" e)) (str (get "message" e)) (lookup "data" (members e)))
    cancelled <- atomically (not <$> isEmptyTMVar (contextCancelled ctx))
    let answer = if cancelled then Left (failure "cancelled" "request cancelled") else case outcome of
          Right value -> Right value
          Left (err :: SomeException) -> Left (fromMaybe (failure "internal" "Internal error") (fromException err))
    respond (either (\err -> ["error" .= publicErrorValue err]) (\value -> ["result" .= value]) answer)

eventLoop :: Peer -> IO ()
eventLoop peer = forever $ do
  frame <- atomically (openSTM peer >> readTBQueue (pEvents peer))
  ctx <- fromFrame frame
  atomically (openSTM peer >> writeTVar (pEventContext peer) (Just ctx))
  flip finally (atomically (writeTVar (pEventContext peer) Nothing)) $ do
    let name = str (get "event" frame); payload = get "data" frame
    let safely action = action `catch` \(_ :: SomeException) -> pure ()
    handlers <- readTVarIO (pEventHandlers peer)
    forM_ (M.lookup name handlers) (\f -> safely (f ctx payload))
    receivers <- readTVarIO (pSubscribers peer)
    mapM_ (\(_, f) -> safely (f ctx name payload)) (M.elems receivers)
    target <- matchReceiver peer name
    let localFrame = Object (KM.delete "event" (case frame of Object xs -> xs; _ -> KM.empty))
    forM_ target $ \(path,r) -> safely (receiverMessage r path (Message localFrame Nothing (Just (toDyn ctx))))

-- Local identifiers are independent of a carrier's role. Reuse the strict
-- profile decoder with the role appropriate to this frame's identifier.
validateWireFrame :: Peer -> Path -> Value -> IO Value
validateWireFrame peer path value = do
  name <- either throwIO pure (encodePath path)
  fields <- case value of Object xs -> pure xs; _ -> throwIO (failure "invalid_message" "Wire frame must be an object")
  when (KM.member "method" fields || KM.member "event" fields)
    (throwIO (failure "invalid_message" "Wire operation names are supplied by its path"))
  let kind = str (get "kind" value)
      routed = case kind of
        "request" -> Object (KM.insert "method" (String name) fields)
        "event" -> Object (KM.insert "event" (String name) fields)
        _ -> value
      ident = str (get "id" value)
      role | kind == "response" = if "s:" `T.isPrefixOf` ident then "server" else "client"
           | otherwise = if "s:" `T.isPrefixOf` ident then "client" else "server"
      bytes = L.toStrict (encode routed)
  when (B.length bytes > maxFrameBytes (pOptions peer)) (throwIO (failure "frame_too_large" "frame exceeds configured limit"))
  either (throwIO . failure "invalid_message") (const (pure routed)) (decodeFrame role bytes)

peerWire :: Peer -> Wire
peerWire peer = Wire
  { wireSend = \path message -> do
      routed <- validateWireFrame peer path (messageFrame message)
      ctx <- maybe (fromFrame (messageFrame message)) pure (messageContext message >>= fromDynamic)
      let frame = messageFrame message; ident = str (get "id" frame); address = messageReturn message
      case str (get "kind" frame) of
        "request" -> mask_ $ do
          returnTo <- maybe (throwIO (failure "invalid_message" "Wire request requires a return address")) pure address
          admitted <- atomically $ do
            (physicalID, pending) <- reserveCall peer ctx (Just (returnTo, ident))
            outgoing <- readTVar (pOutgoing peer)
            if outgoingData outgoing >= queueCapacity (pOptions peer) then do
              writeTVar (pendingCompleted pending) True
              retirePending peer physicalID pending
              pure Nothing
            else do
              let request = Object (KM.insert "id" (String physicalID) (case routed of Object xs -> xs; _ -> KM.empty))
              when (L.length (encode request) > fromIntegral (maxFrameBytes (pOptions peer)))
                (throwSTM (failure "frame_too_large" "frame exceeds configured limit"))
              writeTVar (pOutgoing peer) outgoing {outgoingFrames = outgoingFrames outgoing S.|> (request, Nothing), outgoingData = outgoingData outgoing + 1}
              pure (Just (physicalID, pending, request))
          case admitted of
            Nothing -> finish peer (CloseError 1008 "Wire queue full") >> throwIO WireClosed
            Just (physicalID, pending, request) -> spawnCleanup peer $ do
              result <- try (awaitPending peer ctx physicalID pending request)
              let trace = [(key, value) | key <- ["traceparent", "tracestate"], Just value <- [lookup key (members frame)]]
                  answer = object (["version" .= (1::Int), "kind" .= ("response"::Text), "id" .= ident] ++ trace ++ either (\(e::PublicError) -> ["error" .= publicErrorValue e]) (\v -> ["result" .= v]) result)
              wireSend (returnWire returnTo) [] (Message answer Nothing (messageContext message)) `catch` \(_ :: SomeException) -> pure ()
        "cancel" -> do
          returnTo <- maybe (throwIO (failure "invalid_message" "Wire cancel requires a return address")) pure address
          atomically $ do
            openSTM peer
            calls <- readTVar (pWireCalls peer)
            forM_ calls $ \(WireCall a i physicalID pending) ->
              when (a == returnTo && i == ident) (withdrawPending peer physicalID pending frame)
        "event" -> do
          admitted <- atomically $ do
            openSTM peer
            outgoing <- readTVar (pOutgoing peer)
            if outgoingData outgoing >= queueCapacity (pOptions peer) then pure False else do
              writeTVar (pOutgoing peer) outgoing {outgoingFrames = outgoingFrames outgoing S.|> (routed, Nothing), outgoingData = outgoingData outgoing + 1}
              pure True
          unless admitted (finish peer (CloseError 1008 "Wire queue full") >> throwIO WireClosed)
        _ -> throwIO NoRoute
  , wireReceive = \path receiver -> do
      _ <- either throwIO pure (encodePath path)
      when (null path && not (receiverNamespace receiver)) (throwIO NoRoute)
      key <- newUnique
      let route = (path, receiverNamespace receiver)
      atomically $ do
        openSTM peer
        receivers <- readTVar (pReceivers peer)
        handlers <- readTVar (pHandlers peer)
        events <- readTVar (pEventHandlers peer)
        let name = either (const "") id (encodePath path)
        when (M.member route receivers || (not (receiverNamespace receiver) && (M.member name handlers || M.member name events))) (throwSTM ReceiverExists)
        writeTVar (pReceivers peer) (M.insert route (WireRegistration key receiver) receivers)
      pure $ atomically $ modifyTVar' (pReceivers peer) $ \receivers ->
        case M.lookup route receivers of
          Just (WireRegistration current _) | key == current -> M.delete route receivers
          _ -> receivers
  , wireClose = \code reason -> finish peer (CloseError code reason)
  }
