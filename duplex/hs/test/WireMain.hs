{-# LANGUAGE OverloadedStrings #-}
module Main (main) where

import Control.Concurrent (forkIO)
import Control.Concurrent.MVar
import Control.Exception (throwIO, try)
import Control.Monad (unless)
import Data.Aeson (Value (..))
import Data.Dynamic (fromDynamic, toDyn)
import Data.IORef
import qualified Data.Map.Strict as Map
import Data.Text (Text)
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
  returning <- newReturnAddress root
  otherReturn <- newReturnAddress root
  assert "return capability has stable unique identity" (returning == returning && returning /= otherReturn)
  let message = Message Null (Just returning) (Just (toDyn ("verified" :: Text)))
      selected = at (at root ["outer"]) ["inner"]
  wireSend selected ["tick"] message
  [(path, delivered)] <- readIORef sent
  assert "selection concatenates paths" (path == ["outer", "inner", "tick"])
  assert "selection keeps return identity" (messageReturn delivered == Just returning)
  assert "selection keeps local context" ((messageContext delivered >>= fromDynamic) == Just ("verified" :: Text))
  received <- newIORef []
  detach <- wireReceive selected ["tick"] (Receiver False (\p _ -> modifyIORef' received (++ [p])) (\_ _ -> pure ()))
  deliver ["outer", "inner", "tick"] message
  assert "selected callback is relative to registration origin" . (== [["tick"]]) =<< readIORef received
  detach
  detach
  assert "detach removes child registration" . Map.null =<< readIORef registered
  wireClose selected 1000 "done"
  assert "selected close reaches root" . (== [(1000, "done")]) =<< readIORef rootCloses
  mountTests
  putStrLn "wire tests passed"

mountTests :: IO ()
mountTests = do
  (child, sent, deliver, closes, regs) <- fakeRoot
  mounted <- mount (Map.fromList [("", child)])
  let message = Message Null Nothing Nothing
  expect NoRoute (wireSend mounted [] message)
  expect NoRoute (wireSend mounted ["missing"] message)
  wireSend mounted ["", "a/b"] message
  assert "mount consumes one opaque segment" . (== [["a/b"]]) . map fst =<< readIORef sent
  received <- newIORef []
  endings <- newIORef []
  _ <- wireReceive mounted ["", "tick"] (Receiver False (\p _ -> modifyIORef' received (++ [p])) (\code reason -> modifyIORef' endings (++ [(code, reason)])))
  expect ReceiverExists (wireReceive mounted ["", "tick"] (Receiver False (\_ _ -> pure ()) (\_ _ -> pure ())) >> pure ())
  deliver ["tick"] message
  assert "mount callback restores child segment" . (== [["", "tick"]]) =<< readIORef received
  wireClose mounted 1008 "ended"
  wireClose mounted 1008 "ended"
  assert "mount closes receiver once" . (== [(1008, "ended")]) =<< readIORef endings
  assert "mount close detaches its registration" . Map.null =<< readIORef regs
  assert "mount borrows its children" . null =<< readIORef closes
  expect WireClosed (wireSend mounted ["", "tick"] message)
  wireSend child ["alive"] message
  namespaceTests
  closeDuringRegistration

closeDuringRegistration :: IO ()
closeDuringRegistration = do
  entered <- newEmptyMVar
  release <- newEmptyMVar
  detached <- newIORef (0 :: Int)
  ended <- newIORef (0 :: Int)
  let child = Wire (\_ _ -> pure ())
        (\_ _ -> putMVar entered () >> takeMVar release >> pure (modifyIORef' detached (+ 1)))
        (\_ _ -> pure ())
  mounted <- mount (Map.singleton "child" child)
  result <- newEmptyMVar
  _ <- forkIO $ do
    outcome <- try (wireReceive mounted ["child", "tick"]
      (Receiver False (\_ _ -> pure ()) (\_ _ -> modifyIORef' ended (+ 1))) >> pure ())
    putMVar result outcome
  started <- timeout 1000000 (takeMVar entered)
  assert "registration reaches child" (started == Just ())
  wireClose mounted 1000 "done"
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
  detach <- wireReceive mounted [] (Receiver True (\p _ -> modifyIORef' received (++ [p])) (\c _ -> modifyIORef' ended (++ [c])))
  let message = Message Null Nothing Nothing
  deliverA ["x"] message
  deliverB ["y"] message
  assert "origin namespace receives every child" . (== [["a", "x"], ["b", "y"]]) =<< readIORef received
  aReceiver <- (Map.! []) <$> readIORef regsA
  bReceiver <- (Map.! []) <$> readIORef regsB
  receiverClosed aReceiver 1000 "a"
  assert "one child ending preserves namespace" . null =<< readIORef ended
  receiverClosed bReceiver 1001 "b"
  assert "last child ends namespace once" . (== [1001]) =<< readIORef ended
  detach
  wireClose mounted 1000 "done"
  emptyA <- Map.null <$> readIORef regsA
  emptyB <- Map.null <$> readIORef regsB
  assert "namespace is detached from all children" (emptyA && emptyB)
  assert "closed namespace receives no second ending" . (== [1001]) =<< readIORef ended
  -- Failure on a later child rolls back every already installed receiver.
  blocker <- wireReceive b [] (Receiver True (\_ _ -> pure ()) (\_ _ -> pure ()))
  other <- mount (Map.fromList [("a", a), ("b", b)])
  expect ReceiverExists (wireReceive other [] (Receiver True (\_ _ -> pure ()) (\_ _ -> pure ())) >> pure ())
  assert "failed namespace installation rolls back earlier children" . Map.null =<< readIORef regsA
  blocker

expect :: WireError -> IO () -> IO ()
expect wanted action = do
  result <- try action
  assert ("expected " ++ show wanted) (result == Left wanted)

-- A synchronous inspection double: it never dispatches during send. Tests call
-- deliver explicitly, so root scheduling is independent of view behavior.
fakeRoot :: IO (Wire, IORef [(Path, Message)], Path -> Message -> IO (), IORef [(Int, Text)], IORef (Map.Map Path Receiver))
fakeRoot = do
  sent <- newIORef []
  receivers <- newIORef Map.empty
  closes <- newIORef []
  let wire = Wire
        { wireSend = \path message -> modifyIORef' sent (++ [(path, message)])
        , wireReceive = \path receiver -> do
            existing <- readIORef receivers
            if Map.member path existing then throwIO ReceiverExists else do
              modifyIORef' receivers (Map.insert path receiver)
              pure (modifyIORef' receivers (Map.delete path))
        , wireClose = \code reason -> modifyIORef' closes (++ [(code, reason)])
        }
      deliver path message = do
        existing <- readIORef receivers
        case Map.lookup path existing of
          Just receiver -> receiverMessage receiver path message
          Nothing -> case Map.lookup [] existing of
            Just receiver | receiverNamespace receiver -> receiverMessage receiver path message
            _ -> pure ()
  pure (wire, sent, deliver, closes, receivers)
