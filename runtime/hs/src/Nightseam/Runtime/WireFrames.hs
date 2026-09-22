{-# LANGUAGE OverloadedStrings #-}
module Nightseam.Runtime.WireFrames (frameValue, parseWireFrame) where

import Bitwire
import Data.Aeson
import Data.Aeson.Types (parseEither, Parser)
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy as L
import Data.Text (Text)
import qualified Data.Text as T

-- Serialization is a profile boundary, not part of path composition.
frameValue :: ProfileFrame -> Value
frameValue (ProfileFrame trace body) = object $
  ["version" .= (1 :: Int)] ++ tracing ++ case body of
    Request ident params meta -> ["kind" .= ("request" :: Text), "id" .= ident, "params" .= payload params] ++ metadata meta
    Event value meta -> ["kind" .= ("event" :: Text), "data" .= payload value] ++ metadata meta
    Cancel ident -> ["kind" .= ("cancel" :: Text), "id" .= ident]
    Response ident result -> ["kind" .= ("response" :: Text), "id" .= ident] ++ either
      (\err -> ["error" .= object (["code" .= errorCode err, "message" .= errorMessage err]
        ++ maybe [] (\value -> ["data" .= payload value]) (errorData err))])
      (\value -> ["result" .= payload value]) result
  where
    payload :: JsonPayload -> Value
    payload value = case eitherDecodeStrict' (jsonBytes value) of
      Right parsed -> parsed
      Left problem -> error ("invalid Bitwire JSON payload: " ++ problem)
    metadata = maybe [] (\meta -> ["meta" .= meta])
    tracing = maybe [] (\value -> ["traceparent" .= value]) (traceparent trace)
      ++ maybe [] (\value -> ["tracestate" .= value]) (tracestate trace)

parseWireFrame :: Value -> Either Text ProfileFrame
parseWireFrame = either (Left . T.pack) Right . parseEither (withObject "wire frame" $ \fields -> do
  version <- fields .: "version" :: Parser Int
  if version /= 1 then fail "invalid profile version" else pure ()
  kind <- fields .: "kind" :: Parser Text
  let json key = do
        value <- maybe (fail "missing JSON payload") pure (KM.lookup key fields)
        pure (JsonPayload (L.toStrict (encode value)))
  trace <- Trace <$> fields .:? "traceparent" <*> fields .:? "tracestate"
  body <- case kind of
    "request" -> Request <$> fields .: "id" <*> json "params" <*> fields .:? "meta"
    "event" -> Event <$> json "data" <*> fields .:? "meta"
    "cancel" -> Cancel <$> fields .: "id"
    "response" -> do
      ident <- fields .: "id"
      outcome <- case (KM.lookup "result" fields, KM.lookup "error" fields) of
        (Just value, Nothing) -> pure (Right (JsonPayload (L.toStrict (encode value))))
        (Nothing, Just value) -> Left <$> withObject "profile error" (\err ->
          ProfileError <$> err .: "code" <*> err .: "message"
            <*> pure (JsonPayload . L.toStrict . encode <$> KM.lookup "data" err)) value
        _ -> fail "response requires exactly one result or error"
      pure (Response ident outcome)
    _ -> fail "invalid profile kind"
  pure (ProfileFrame trace body))
