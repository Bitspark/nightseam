{-# LANGUAGE OverloadedStrings #-}
module Main (main) where

import Control.Monad (forM, unless)
import Data.Aeson (Value(..), encode)
import Data.Aeson.Key (Key)
import qualified Data.Aeson.Key as Key
import qualified Data.Aeson.KeyMap as K
import qualified Data.ByteString as B
import qualified Data.ByteString.Lazy as L
import Data.Either (isRight)
import qualified Data.Text as T
import qualified Data.Text.Encoding as T
import qualified Data.Vector as V
import Nightseam.Runtime.Validate
import System.Environment (getArgs)
import System.Exit (exitFailure)

get :: Key -> Value -> Maybe Value
get k (Object o) = K.lookup k o
get _ _ = Nothing
field :: Key -> Value -> Value
field k = maybe Null id . get k
rows :: Key -> Value -> [Value]
rows k v = case field k v of Array a -> V.toList a; _ -> []
str :: Value -> T.Text
str (String s) = s
str _ = ""
truth :: Value -> Bool
truth v = v == Bool True

main :: IO ()
main = do
  args <- getArgs
  let root = case args of r:_ -> r; _ -> "../.."
  table <- readTable (root ++ "/conformance/tables/validator.json")
  frames <- readTable (root ++ "/conformance/tables/frames.json")
  unicode <- readTable (root ++ "/conformance/tables/unicode.json")
  examples <- readTable (root ++ "/conformance/tables/examples.json")
  let run expression slots = validate (field "wire" table) (field "imported" table) slots expression
  results <- forM (zip [0 :: Int ..] (rows "cases" table)) $ \(i,row) -> do
    let actual = run (field "expression" row) (field "slots" row) (field "value" row)
        expected = truth (field "valid" row)
        message = str (field "message" row)
    check ("validator " ++ show i ++ " " ++ show (field "expression" row))
      (isRight actual == expected && (T.null message || actual == Left message)) (show actual)
  pats <- forM (rows "patterns" table) $ \row ->
    check ("pattern " ++ show (field "pattern" row))
      (isRight (validatePattern (str (field "pattern" row))) == truth (field "valid" row)) "syntax verdict"
  matches <- forM (rows "patternValues" table) $ \row ->
    check ("pattern value " ++ show row)
      (matchPattern (str (field "pattern" row)) (str (field "value" row)) == truth (field "valid" row)) "match verdict"
  laws <- fmap concat $ forM (rows "equivalence" table) $ \row ->
    forM (rows "values" row) $ \value -> check ("equivalence " ++ show row)
      (isRight (run (field "generic" row) Null value) == isRight (run (field "bound" row) Null value)) "verdict differs"
  envelopes <- fmap concat $ forM (rows "rows" frames) $ \row -> do
    let roles = case str (field "to" row) of "either" -> ["server", "client"]; r -> [r]
    forM roles $ \role -> do
      let actual = decodeFrame role (T.encodeUtf8 (str (field "frame" row)))
      check ("frame " ++ T.unpack (str (field "name" row)) ++ " to " ++ T.unpack role)
        (isRight actual == truth (field "valid" row)) (show actual)
  scalars <- forM (rows "rows" unicode) $ \row -> do
    let raw = T.encodeUtf8 (str (field "raw" row))
    check ("Unicode " ++ T.unpack (str (field "name" row)))
      (isRight (strictJSON raw) == truth (field "valid" row)
        && isRight (decodeFrame "server" ("{\"version\":1,\"kind\":\"event\",\"event\":\"probe\",\"data\":" <> raw <> "}")) == truth (field "valid" row)) "scalar verdict"
  documents <- forM (rows "rows" examples) $ \row -> do
    let label = T.unpack (str (field "corpus" row) <> "/" <> str (field "family" row) <> "/" <> str (field "path" row))
    case get "unavailable" row of
      Just unavailable -> check label (get "value" row == Nothing && not (T.null (str (field "Reason" unavailable))) && field "Kind" unavailable `elem` [String "limit",String "impossible"]) "invalid unavailability"
      _ -> do
        let schemas = field (Key.fromText (str (field "corpus" row))) (field "schemas" examples)
            descriptor = field (Key.fromText (str (field "family" row))) schemas
            to = str (field "to" row)
            member = str (field "member" row)
            value = if T.null member then field "value" row else field (Key.fromText member) (field "value" row)
            bindings = case field "bindings" row of
              Object slots -> Object (K.map (\slot -> Object (K.fromList [("type",v) | Just v <- [get "Type" slot]] <> K.fromList [("family",v) | Just v <- [get "Family" slot]])) slots)
              _ -> Null
            actual = do
              unless (T.null to) $ do
                _ <- decodeFrame to (L.toStrict (encode (field "value" row)))
                pure ()
              case get "expression" row of
                Nothing -> pure ()
                Just expr -> validate descriptor schemas bindings expr value
        check label (isRight actual) (show actual)
  extras <- sequence
    [ check "strict trailing JSON" (not (isRight (strictJSON "true false"))) "accepted"
    , check "strict invalid UTF-8" (not (isRight (strictJSON (B.pack [34,255,34])))) "accepted"
    , check "strict overwritten invalid scalar" (not (isRight (strictJSON "{\"x\":\"\\uD800\",\"x\":0}"))) "accepted"
    , check "strict huge number preserved" (isRight (strictJSON "1e400")) "refused"
    , check "strict nested duplicate allowed" (isRight (decodeFrame "server" "{\"version\":1,\"kind\":\"event\",\"event\":\"x\",\"data\":{\"x\":1,\"x\":2}}")) "refused"
    , check "large nullable repetition reaches fixed point" (matchPattern "^(?:a?){999999999999999999999999}$" "aaa") "refused"
    , check "counted pattern beyond native expansion" (matchPattern "^(?:a{500}){3}$" (T.replicate 1500 "a")) "refused"
    , check "trace form remains opaque" (isRight (decodeFrame "server" "{\"version\":1,\"kind\":\"event\",\"event\":\"x\",\"data\":null,\"traceparent\":\"ff-00000000000000000000000000000000-0000000000000000-00\"}")) "refused"
    ]
  numeric <- forM [("9007199254740991",True),("9007199254740991.0",True),("9007199254740991.1",False),("9007199254740992",False),("1e-1000000",False),("-0e-1000000",True),("1.5",False),("1000e-3",True)] $ \(raw,expected) -> do
    let actual = strictJSON raw >>= run (String "integer") Null
    check ("integer precision " ++ show raw) (isRight actual == expected) (show actual)
  drawn <- forM [("\"text\"",True),("12",False)] $ \(raw,expected) -> do
    let actual = do
          slots <- strictJSON "{\"S.Envelope\":{\"type\":\"string\"}}"
          expr <- strictJSON "{\"apply\":\"peer.Box\",\"with\":{\"S\":\"S\"}}"
          val <- strictJSON ("{\"item\":" <> raw <> "}")
          run expr slots val
    check ("drawn type forwarded as family " ++ show raw) (isRight actual == expected) (show actual)
  let allResults = results ++ pats ++ matches ++ laws ++ envelopes ++ scalars ++ documents ++ extras ++ numeric ++ drawn
  putStrLn (show (length (filter id allResults)) ++ "/" ++ show (length allResults) ++ " validator/frame checks passed")
  unless (and allResults) exitFailure

readTable :: FilePath -> IO Value
readTable path = do
  contents <- B.readFile path
  either (fail . T.unpack) pure (strictJSON contents)

check :: String -> Bool -> String -> IO Bool
check label ok detail = do
  unless ok (putStrLn ("FAIL " ++ label ++ ": " ++ detail))
  pure ok
