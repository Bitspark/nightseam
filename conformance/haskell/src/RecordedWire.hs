{-# LANGUAGE OverloadedStrings #-}
-- This is a consumer composition acceptance witness, not a record/follow API.
-- Store append and subscriber handoff share one lock. Every subscriber owns its
-- bounded writer, while the production Wire views provide selection and mounts.
module RecordedWire (recordedWireWitness) where

import Control.Concurrent (forkIO)
import Control.Concurrent.MVar
import Control.Concurrent.STM
import Control.Exception (bracket, finally, throwIO, try)
import Control.Monad (forM_, unless, when)
import Data.Aeson (Value (..), object, (.=))
import qualified Data.Aeson.KeyMap as KM
import qualified Data.Map.Strict as Map
import qualified Data.Sequence as Seq
import Data.Foldable (toList)
import Data.Text (Text)
import Bitwire hiding (errorCode, errorMessage, errorData)
import Nightseam.Runtime.WireFrames
import Nightseam.Duplex.Dispatcher
import Nightseam.Duplex.Wire
import System.Timeout (timeout)

data Entry = Entry Path Message Int

data Follower = Follower
  { followerLive :: TBQueue Entry
  , followerStop :: TVar Bool
  , followerDone :: TMVar ()
  , followerSent :: TBQueue Int
  , followerPaused :: TMVar ()
  , followerResume :: TMVar ()
  }

data StoreState = StoreState
  { storedEntries :: Seq.Seq Entry
  , storedFollowers :: Map.Map Int Follower
  , nextFollower :: Int
  }

newtype Store = Store (MVar StoreState)

newStore :: IO Store
newStore = Store <$> newMVar (StoreState Seq.empty Map.empty 0)

storeHead :: Store -> IO Int
storeHead (Store state) = withMVar state (pure . Seq.length . storedEntries)

storeWire :: Store -> Wire
storeWire (Store state) = Wire
  { send = \path message -> modifyMVar_ state $ \current -> do
      let sequenceNumber = Seq.length (storedEntries current) + 1
          entry = Entry path message sequenceNumber
      followers <- fmap Map.fromList $ fmap concat $ mapM (admit entry) (Map.toList (storedFollowers current))
      pure current {storedEntries = storedEntries current Seq.|> entry, storedFollowers = followers}
  }
  where
    admit entry (identifier, follower) = atomically $ do
      full <- isFullTBQueue (followerLive follower)
      if full then do
        -- Signal only: ending the subscriber runs in its own writer and never
        -- invokes application code while append exclusion is held.
        writeTVar (followerStop follower) True
        pure []
      else do
        writeTBQueue (followerLive follower) entry
        pure [(identifier, follower)]

closeStore :: Store -> IO ()
closeStore (Store state) = modifyMVar_ state $ \current -> do
  forM_ (Map.elems (storedFollowers current)) (\follower -> atomically (writeTVar (followerStop follower) True))
  pure current {storedFollowers = Map.empty}

attach :: Store -> Int -> Endpoint -> Int -> Bool -> IO (Int, Follower)
attach (Store state) after target bound pause = do
  follower <- Follower <$> newTBQueueIO (fromIntegral bound) <*> newTVarIO False <*> newEmptyTMVarIO
    <*> newTBQueueIO 32 <*> newEmptyTMVarIO <*> newEmptyTMVarIO
  (headValue, history) <- modifyMVar state $ \current -> do
    let headValue = Seq.length (storedEntries current)
        history = toList (Seq.drop after (storedEntries current))
        identifier = nextFollower current
    pure (current
      { storedFollowers = Map.insert identifier follower (storedFollowers current)
      , nextFollower = identifier + 1
      }, (headValue, history))
  let stopped = readTVar (followerStop follower) >>= check
      sendEntry (Entry path message sequenceNumber) = do
        stop <- readTVarIO (followerStop follower)
        if stop then pure False else do
          send (endpointWire target) path message
          atomically (writeTBQueue (followerSent follower) sequenceNumber)
          pure True
      replay _ [] = follow
      replay index (entry : rest) = do
        sent <- sendEntry entry
        when sent $ do
          continue <- if index == (0 :: Int) && pause then do
            atomically (putTMVar (followerPaused follower) ())
            atomically ((stopped >> pure False) `orElse` (readTMVar (followerResume follower) >> pure True))
          else pure True
          when continue (replay (index + 1) rest)
      follow = do
        entry <- atomically ((stopped >> pure Nothing) `orElse` (Just <$> readTBQueue (followerLive follower)))
        case entry of
          Nothing -> pure ()
          Just item -> sendEntry item >>= \sent -> when sent follow
      finish = close target (Code 1008) "recorded handoff ended" `finally` atomically (putTMVar (followerDone follower) ())
  _ <- forkIO (replay 0 history `finally` finish)
  pure (headValue, follower)

stopFollower :: Follower -> IO ()
stopFollower follower = atomically (writeTVar (followerStop follower) True) >> atomically (readTMVar (followerDone follower))

awaitSent :: Follower -> Int -> IO ()
awaitSent follower lastSequence = do
  sent <- atomically (readTBQueue (followerSent follower))
  unless (sent == lastSequence) (awaitSent follower lastSequence)

-- A fixture root supplies bounded asynchronous delivery. Its receivers may
-- reenter the store, proving callback execution is outside append exclusion.
newRoot :: IO Endpoint
newRoot = do
  receivers <- newMVar (Map.empty :: Map.Map Path Receiver)
  queue <- newTBQueueIO 16
  stopped <- newTVarIO False
  done <- newEmptyTMVarIO
  let run = do
        next <- atomically ((readTVar stopped >>= check >> pure Nothing) `orElse` (Just <$> readTBQueue queue))
        case next of
          Nothing -> pure ()
          Just (Entry path message _) -> do
            receiver <- withMVar receivers (pure . Map.lookup [])
            forM_ receiver (\held -> maybe (pure ()) (\f -> f path message) (onMessage held))
            run
  _ <- forkIO (run `finally` atomically (putTMVar done ()))
  pure Endpoint
    { endpointWire = Wire $ \path message -> atomically $ do
        closed <- readTVar stopped
        when closed (throwSTM WireClosed)
        full <- isFullTBQueue queue
        when full (throwSTM (userError "recorded witness output queue full"))
        writeTBQueue queue (Entry path message 0)
    , receive = \receiver -> modifyMVar receivers $ \current ->
        if Map.member [] current then throwIO ReceiverExists else
          pure (Map.insert [] receiver current, modifyMVar_ receivers (pure . Map.delete []))
    , close = \_ _ -> atomically (writeTVar stopped True) >> atomically (readTMVar done)
    }

data Presentation = Presentation
  { presentationWire :: Endpoint
  , presentationRoot :: Endpoint
  , presentationEnd :: Endpoint
  , presentationRoutes :: [Dispatcher]
  , presentationValues :: TQueue Int
  , presentationClosed :: TQueue Int
  , presentationErrors :: TQueue Text
  , presentationCallbacks :: TVar Int
  }

present :: Store -> IO Presentation
present store = do
  root <- newRoot
  end <- newRoot
  rootRoutes <- newDispatcher root
  endRoutes <- newDispatcher end
  endView <- selectEndpoint endRoutes ["destination"]
  destinationMount <- mount (Map.singleton "out" endView)
  destinationRoutes <- newDispatcher destinationMount
  destination <- selectEndpoint destinationRoutes ["out"]
  rootView <- selectEndpoint rootRoutes ["source"]
  inner <- mount (Map.singleton "in" rootView)
  originMount <- mount (Map.singleton "outer" inner)
  originRoutes <- newDispatcher originMount
  origin <- selectEndpoint originRoutes ["outer", "in"]
  values <- newTQueueIO
  closed <- newTQueueIO
  errors <- newTQueueIO
  callbacks <- newTVarIO 0
  _ <- receive destination (Receiver (Just (\path message -> do
      if path /= ["tick"] then atomically (writeTQueue errors "recorded destination path mismatch") else do
        -- Must acquire the same lock as append. A callback on the producer's
        -- stack under that lock deadlocks and fails the witness deadline.
        _ <- storeHead store
        case messageNumber message of
          Nothing -> atomically (writeTQueue errors "recorded destination data is not an integer")
          Just value -> atomically $ do
            modifyTVar' callbacks (+ 1)
            writeTQueue values value)) (Just (\_ _ -> pure ())))
  _ <- receive origin (Receiver (Just (send (endpointWire destination))) (Just (\code _ -> do
      _ <- storeHead store
      atomically (writeTQueue closed (unCode code)))))
  pure (Presentation origin root end [rootRoutes, endRoutes, destinationRoutes, originRoutes] values closed errors callbacks)

closePresentation :: Presentation -> IO ()
closePresentation presentation = do
  forM_ (presentationRoutes presentation) (\routes -> closeDispatcher routes (Code 1000) "done")
  close (presentationWire presentation) (Code 1000) "done"
  close (presentationRoot presentation) (Code 1000) "done"
  close (presentationEnd presentation) (Code 1000) "done"

recordedMessage :: Int -> Message
recordedMessage value = Message (frame (object ["version" .= (1 :: Int), "kind" .= ("event" :: Text), "data" .= value])) Nothing

messageNumber :: Message -> Maybe Int
messageNumber message = case frameValue (messageFrame message) of
  Object fields -> case KM.lookup "data" fields of
    Just (Number number) -> let integer = truncate number :: Int
      in if fromIntegral integer == number then Just integer else Nothing
    _ -> Nothing
  _ -> Nothing

collect :: Presentation -> IO [Int]
collect presentation = do
  send (endpointWire (presentationWire presentation)) ["tick"] (recordedMessage 0)
  let loop values = do
        next <- atomically ((Left <$> readTQueue (presentationErrors presentation)) `orElse`
          (Right <$> readTQueue (presentationValues presentation)))
        case next of
          Left problem -> fail (show problem)
          Right 0 -> pure (reverse values)
          Right value -> loop (value : values)
  loop []

headCase :: Bool -> IO Value
headCase before = bracket newStore closeStore $ \store ->
  bracket (present store) closePresentation $ \presentation -> do
    let source = at (storeWire store) []
    let appendValue value = send source ["tick"] (recordedMessage value)
    mapM_ appendValue [1 .. 3]
    when before (appendValue 4)
    bracket (attach store 0 (presentationWire presentation) 2 True) (stopFollower . snd) $ \(headValue, follower) -> do
      atomically (readTMVar (followerPaused follower))
      produced <- newEmptyTMVarIO
      _ <- forkIO (mapM_ appendValue [(if before then 5 else 4) .. 5] >> atomically (putTMVar produced ()))
      -- Producer completion is observed before releasing the paused replay.
      atomically (readTMVar produced)
      producerProgress <- atomically (isEmptyTMVar (followerResume follower))
      atomically (putTMVar (followerResume follower) ())
      awaitSent follower 5
      appendValue 6
      awaitSent follower 6
      first <- collect presentation
      firstCallbacks <- readTVarIO (presentationCallbacks presentation)
      bracket (present store) closePresentation $ \second ->
        bracket (attach store 3 (presentationWire second) 2 False) (stopFollower . snd) $ \(_, late) -> do
          awaitSent late 6
          after <- collect second
          secondCallbacks <- readTVarIO (presentationCallbacks second)
          pure (object
            [ "cut" .= (if before then "append_before_head" else "head_before_append" :: Text)
            , "head" .= headValue, "first" .= first, "after_three" .= after
            , "producer_progress" .= producerProgress
            , "callbacks_outside_append" .= (firstCallbacks == length first + 1 && secondCallbacks == length after + 1)
            ])

stallCase :: IO Value
stallCase = bracket newStore closeStore $ \store -> do
  let appendValue value = send (storeWire store) ["tick"] (recordedMessage value)
  mapM_ appendValue [1 .. 3]
  bracket (present store) closePresentation $ \stalled ->
    bracket (present store) closePresentation $ \healthy ->
      bracket (attach store 0 (presentationWire stalled) 2 True) (stopFollower . snd) $ \(_, slow) -> do
        atomically (readTMVar (followerPaused slow))
        bracket (attach store 3 (presentationWire healthy) 2 False) (stopFollower . snd) $ \(_, fast) -> do
          forM_ [4 .. 5] (\value -> appendValue value >> awaitSent fast value)
          queued <- atomically (lengthTBQueue (followerLive slow))
          appendValue 6
          awaitSent fast 6
          atomically (readTMVar (followerDone slow))
          code <- atomically (readTQueue (presentationClosed stalled))
          remainingCloses <- atomically (flushTQueue (presentationClosed stalled))
          refused <- try (send (endpointWire (presentationWire stalled)) ["tick"] (recordedMessage 99))
          unless (refused == Left WireClosed) (fail "stalled carrier accepted after close")
          underneath <- newEmptyTMVarIO
          _ <- register (head (presentationRoutes stalled)) ["probe"] (Receiver (Just (\_ message -> forM_ (messageNumber message) (atomically . putTMVar underneath))) (Just (\_ _ -> pure ())))
          send (endpointWire (presentationRoot stalled)) ["probe"] (recordedMessage 99)
          probe <- atomically (readTMVar underneath)
          appendValue 7
          awaitSent fast 7
          values <- collect healthy
          headValue <- storeHead store
          pure (object
            [ "bound" .= (2 :: Int), "queued_at_bound" .= queued
            , "closed" .= (1 + length remainingCloses), "close_code" .= code
            , "healthy" .= values, "underneath" .= [probe], "head" .= headValue
            , "producer_progress" .= (headValue == 7 && not (null values) && last values == 7)
            ])

recordedWireWitness :: IO Value
recordedWireWitness = do
  result <- timeout 5000000 $ do
    cases <- mapM headCase [False, True]
    stalled <- stallCase
    pure (object ["cases" .= cases, "stalled" .= stalled])
  maybe (fail "recorded wire witness timed out") pure result

frame :: Value -> ProfileFrame
frame = either (error . show) id . parseWireFrame
