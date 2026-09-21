{-# LANGUAGE OverloadedStrings #-}
module Main where
import Control.Concurrent.Async
import Control.Concurrent (threadDelay)
import Control.Concurrent.STM
import Control.Exception (try)
import Control.Monad
import Data.Aeson hiding (Options, defaultOptions)
import Nightseam.Duplex
import Nightseam.Runtime
import System.Timeout

main :: IO ()
main = do
  (a,b) <- pipe 1048576
  client <- newPeer a Client defaultOptions
  server <- newPeer b Server defaultOptions
  handle server "echo" (\_ _ value -> pure value)
  ctx <- newCallContext
  result <- call client ctx "echo" (object ["answer" .= (42 :: Int)])
  unless (result == object ["answer" .= (42 :: Int)]) (error "round trip")
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
