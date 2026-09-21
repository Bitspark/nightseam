{-# LANGUAGE OverloadedStrings #-}
module Main where
import Control.Concurrent.Async
import Control.Exception
import Control.Monad
import Nightseam.Duplex
import qualified Nightseam.Duplex.WebSocket as WS
import System.Timeout

check :: String -> Bool -> IO ()
check label ok = unless ok (error label)

main :: IO ()
main = do
  (a,b) <- pipe 1024
  mapM_ (sendFrame a . Frame TextFrame) ["one", "two", "three"]
  closeConnection a 4001 "finished"
  values <- replicateM 3 (receiveFrame b)
  check "ordered draining before close" (map frameData values == ["one", "two", "three"])
  ended <- try (receiveFrame b) :: IO (Either CloseError Frame)
  check "remote close code and reason" (ended == Left (CloseError 4001 "finished"))
  (c,d) <- pipe 1024
  replicateM_ 8 (sendFrame c (Frame TextFrame "bounded"))
  refused <- timeout 20000 (sendFrame c (Frame TextFrame "waiting"))
  check "pipe bound paces sender" (refused == Nothing)
  _ <- receiveFrame d
  resumed <- timeout 20000 (sendFrame c (Frame BinaryFrame "next"))
  check "receiving releases capacity" (resumed == Just ())
  waiter <- async (receiveFrame c)
  abortConnection d
  result <- waitCatch waiter
  check "abort wakes receiver" (case result of Left _ -> True; _ -> False)
  listener <- WS.listen 1024 []
  pendingAccept <- async (try (WS.accept listener) :: IO (Either CloseError Connection))
  stopped <- timeout 1000000 (WS.closeListener listener)
  check "listener shutdown ends its worker" (stopped == Just ())
  acceptedAfterClose <- timeout 1000000 (wait pendingAccept)
  let closedAccept outcome = case outcome of Just (Left (CloseError 1000 _)) -> True; _ -> False
  check "listener shutdown wakes a pending public accept" (closedAccept acceptedAfterClose)
  futureAccept <- timeout 1000000 (try (WS.accept listener) :: IO (Either CloseError Connection))
  check "accept refuses an already closed listener" (closedAccept futureAccept)
  listener2 <- WS.listen 1024 []
  socketClient <- WS.dial (WS.listenerURL listener2) 1024 []
  socketServer <- WS.accept listener2
  WS.closeListener listener2
  sendFrame socketClient (Frame TextFrame "socket frame")
  received <- timeout 1000000 (receiveFrame socketServer)
  check "closing a listener preserves accepted WebSocket carriers" (received == Just (Frame TextFrame "socket frame"))
  closeConnection socketServer 4002 "socket ended"
  socketEnd <- timeout 1000000 (try (receiveFrame socketClient) :: IO (Either CloseError Frame))
  check "WebSocket close code and reason" (socketEnd == Just (Left (CloseError 4002 "socket ended")))
  WS.closeListener listener2
  putStrLn "seam tests passed"
