{-# LANGUAGE DeriveDataTypeable #-}
module Nightseam.Duplex
  (Frame(..), FrameKind(..), CloseError(..), Connection(..), pipe) where

import Control.Concurrent.STM
import Control.Exception
import Control.Monad
import Data.ByteString (ByteString)
import qualified Data.ByteString as B
import Data.Text (Text)
import Data.Typeable

data FrameKind = TextFrame | BinaryFrame deriving (Eq, Show)
data Frame = Frame { frameKind :: FrameKind, frameData :: ByteString } deriving (Eq, Show)
data CloseError = CloseError { closeCode :: Int, closeReason :: Text } deriving (Eq, Show, Typeable)
instance Exception CloseError

-- Every method is interruptible. A caller can impose its own deadline with
-- timeout, without turning a timed-out receive into a transport close.
data Connection = Connection
  { sendFrame :: Frame -> IO ()
  , receiveFrame :: IO Frame
  , closeConnection :: Int -> Text -> IO ()
  , abortConnection :: IO ()
  , connectionSubprotocol :: Text
  }

pipe :: Int -> IO (Connection, Connection)
pipe limit = do
  qa <- newTBQueueIO 8
  qb <- newTBQueueIO 8
  da <- newEmptyTMVarIO
  db <- newEmptyTMVarIO
  let end d e = atomically (void (tryPutTMVar d e))
      failIfEnded d = do
        value <- tryReadTMVar d
        maybe (pure ()) throwSTM value
      make output input own remote = Connection
        { sendFrame = \frame -> atomically $ do
            failIfEnded own
            failIfEnded remote
            writeTBQueue output frame
        , receiveFrame = do
            frame <- atomically $ do
              failIfEnded own
              readTBQueue input `orElse` (readTMVar remote >>= throwSTM)
            when (limit > 0 && B.length (frameData frame) > limit) $ do
              end own (CloseError 1009 mempty)
              throwIO (CloseError 1009 mempty)
            pure frame
        , closeConnection = \code reason -> end own (CloseError code reason)
        , abortConnection = end own (CloseError 1006 mempty)
        , connectionSubprotocol = mempty
        }
  pure (make qa qb da db, make qb qa db da)
