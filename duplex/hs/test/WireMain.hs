{-# LANGUAGE OverloadedStrings #-}
module Main (main) where

import Control.Concurrent (forkIO)
import Control.Concurrent.MVar
import Control.Exception (throwIO, try)
import Control.Monad (unless)
import Data.Aeson (Value (..))
import Data.IORef
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import Bitwire
import Nightseam.Duplex.Dispatcher
import Nightseam.Duplex.Wire
import System.Timeout (timeout)

assert :: String -> Bool -> IO ()
assert label passed = unless passed (fail label)

main :: IO ()
main = do
  let paths = [[], [""], ["a/b"], ["a", "b"], ["é", "😀", ""], ["e\x0301"], [".", "..", "/"]]
  mapM_ (\path -> assert "canonical path round trip" ((encodePath path >>= decodePath) == Right path)) paths
  assert "empty origin and segment differ" (encodePath [] == Right "" && encodePath [""] == Right "0:")
  assert "UTF-8 byte length" (encodePath ["é", "😀"] == Right "2:é4:😀")
  assert "no normalization" (encodePath ["é"] /= encodePath ["e\x0301"])
  mapM_ (\encoded -> assert "noncanonical path rejected" (decodePath encoded == Left InvalidPath))
    [":", "01:a", "1", "-1:a", "+1:a", "1:é", "3:é", "2:a", "9999999999999999999999999999999999999999:x", "1:a:"]
  (root, sent, deliver, rootCloses, registered) <- fakeRoot
  returning <- newReturnAddress (endpointWire root)
  otherReturn <- newReturnAddress (endpointWire root)
  assert "return capability has stable unique identity" (returning == returning && returning /= otherReturn)
  let message = Message (ProfileFrame (Trace Nothing Nothing) (Event (JsonPayload "null") Nothing)) (Just returning)
      selected = at (at (endpointWire root) ["outer"]) ["inner"]
  send selected ["tick"] message
  [(path, delivered)] <- readIORef sent
  assert "selection concatenates paths" (path == ["outer", "inner", "tick"])
  assert "selection keeps return identity" (messageReturn delivered == Just returning)
  received <- newIORef []
  routes <- newDispatcher root
  endpoint <- selectEndpoint routes ["outer", "inner"]
  detach <- receive endpoint (Receiver (Just (\p _ -> modifyIORef' received (++ [p]))) (Just (\_ _ -> pure ())))
  deliver ["outer", "inner", "tick"] message
  assert "selected callback is relative to registration origin" . (== [["tick"]]) =<< readIORef received
  detach
  detach
  close endpoint (Code 1000) "done"
  closeDispatcher routes (Code 1000) "done"
  assert "detach removes child registration" . Map.null =<< readIORef registered
  assert "selected close leaves borrowed root open" . null =<< readIORef rootCloses
  mountTests
  putStrLn "wire tests passed"

mountTests :: IO ()
mountTests = do
  (child, sent, deliver, closes, regs) <- fakeRoot
  mounted <- mount (Map.fromList [("", child)])
  let message = Message (ProfileFrame (Trace Nothing Nothing) (Event (JsonPayload "null") Nothing)) Nothing
  expect NoRoute (send (endpointWire mounted) [] message)
  expect NoRoute (send (endpointWire mounted) ["missing"] message)
  send (endpointWire mounted) ["", "a/b"] message
  assert "mount consumes one opaque segment" . (== [["a/b"]]) . map fst =<< readIORef sent
  received <- newIORef []
  endings <- newIORef []
  _ <- receive mounted (Receiver (Just (\p _ -> modifyIORef' received (++ [p]))) (Just (\code reason -> modifyIORef' endings (++ [(unCode code, reason)]))))
  expect ReceiverExists (receive mounted (Receiver (Just (\_ _ -> pure ())) (Just (\_ _ -> pure ()))) >> pure ())
  deliver ["tick"] message
  assert "mount callback restores child segment" . (== [["", "tick"]]) =<< readIORef received
  close mounted (Code 1008) "ended"
  close mounted (Code 1008) "ended"
  assert "mount closes receiver once" . (== [(1008, "ended")]) =<< readIORef endings
  assert "mount close detaches its registration" . Map.null =<< readIORef regs
  assert "mount borrows its children" . null =<< readIORef closes
  expect WireClosed (send (endpointWire mounted) ["", "tick"] message)
  send (endpointWire child) ["alive"] message
  namespaceTests
  closeDuringRegistration

closeDuringRegistration :: IO ()
closeDuringRegistration = do
  entered <- newEmptyMVar
  release <- newEmptyMVar
  detached <- newIORef (0 :: Int)
  ended <- newIORef (0 :: Int)
  let child = Endpoint (Wire (\_ _ -> pure ()))
        (\_ -> putMVar entered () >> takeMVar release >> pure (modifyIORef' detached (+ 1)))
        (\_ _ -> pure ())
  mounted <- mount (Map.singleton "child" child)
  result <- newEmptyMVar
  _ <- forkIO $ do
    outcome <- try (receive mounted
      (Receiver (Just (\_ _ -> pure ())) (Just (\_ _ -> modifyIORef' ended (+ 1)))) >> pure ())
    putMVar result outcome
  started <- timeout 1000000 (takeMVar entered)
  assert "registration reaches child" (started == Just ())
  close mounted (Code 1000) "done"
  putMVar release ()
  outcome <- timeout 1000000 (takeMVar result)
  assert "closure refuses registration whose install was in flight" (outcome == Just (Left WireClosed))
  assert "late installed child registration is detached once" . (== 1) =<< readIORef detached
  assert "closure notifies in-flight receiver once" . (== 1) =<< readIORef ended

namespaceTests :: IO ()
namespaceTests = do
  (a, _, deliverA, _, regsA) <- fakeRoot
  (b, _, deliverB, _, regsB) <- fakeRoot
  mounted <- mount (Map.fromList [("a", a), ("b", b)])
  received <- newIORef []
  ended <- newIORef []
  detach <- receive mounted (Receiver (Just (\p _ -> modifyIORef' received (++ [p]))) (Just (\c _ -> modifyIORef' ended (++ [unCode c]))))
  let message = Message (ProfileFrame (Trace Nothing Nothing) (Event (JsonPayload "null") Nothing)) Nothing
  deliverA ["x"] message
  deliverB ["y"] message
  assert "origin namespace receives every child" . (== [["a", "x"], ["b", "y"]]) =<< readIORef received
  aReceiver <- (Map.! []) <$> readIORef regsA
  bReceiver <- (Map.! []) <$> readIORef regsB
  maybe (pure ()) (\f -> f (Code 1000) "a") (onClosed aReceiver)
  assert "one child ending preserves namespace" . null =<< readIORef ended
  maybe (pure ()) (\f -> f (Code 1001) "b") (onClosed bReceiver)
  assert "last child ends namespace once" . (== [1001]) =<< readIORef ended
  detach
  close mounted (Code 1000) "done"
  emptyA <- Map.null <$> readIORef regsA
  emptyB <- Map.null <$> readIORef regsB
  assert "namespace is detached from all children" (emptyA && emptyB)
  assert "closed namespace receives no second ending" . (== [1001]) =<< readIORef ended
  -- Failure on a later child rolls back every already installed receiver.
  blocker <- receive b (Receiver (Just (\_ _ -> pure ())) (Just (\_ _ -> pure ())))
  other <- mount (Map.fromList [("a", a), ("b", b)])
  expect ReceiverExists (receive other (Receiver (Just (\_ _ -> pure ())) (Just (\_ _ -> pure ()))) >> pure ())
  assert "failed namespace installation rolls back earlier children" . Map.null =<< readIORef regsA
  blocker

expect :: WireError -> IO () -> IO ()
expect wanted action = do
  result <- try action
  assert ("expected " ++ show wanted) (result == Left wanted)

-- A synchronous inspection double: it never dispatches during send. Tests call
-- deliver explicitly, so root scheduling is independent of view behavior.
fakeRoot :: IO (Endpoint, IORef [(Path, Message)], Path -> Message -> IO (), IORef [(Int, Text)], IORef (Map.Map Path Receiver))
fakeRoot = do
  sent <- newIORef []
  receivers <- newIORef Map.empty
  closes <- newIORef []
  let wire = Endpoint
        { endpointWire = Wire $ \path message -> modifyIORef' sent (++ [(path, message)])
        , receive = \receiver -> do
            let path = []
            existing <- readIORef receivers
            if Map.member path existing then throwIO ReceiverExists else do
              modifyIORef' receivers (Map.insert path receiver)
              pure (modifyIORef' receivers (Map.delete path))
        , close = \code reason -> modifyIORef' closes (++ [(unCode code, reason)])
        }
      deliver path message = do
        existing <- readIORef receivers
        case Map.lookup [] existing of
          Just receiver -> maybe (pure ()) (\f -> f path message) (onMessage receiver)
          Nothing -> pure ()
  pure (wire, sent, deliver, closes, receivers)
