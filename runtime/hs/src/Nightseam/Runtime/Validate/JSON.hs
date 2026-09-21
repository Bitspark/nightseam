{-# LANGUAGE OverloadedStrings #-}
module Nightseam.Runtime.Validate.JSON (strictJSON, rawJSON, Raw(..), valueOf) where

import Control.Applicative (many)
import Control.Monad (replicateM, unless)
import Data.Aeson (Value(..), eitherDecodeStrict')
import qualified Data.Aeson.Key as K
import qualified Data.Aeson.KeyMap as KM
import qualified Data.Attoparsec.Text as A
import Data.ByteString (ByteString)
import Data.Char (chr, digitToInt, isHexDigit, ord)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as T
import qualified Data.Vector as V

-- Keep members until the envelope has been checked. Even discarded duplicate
-- payload values must first pass the Unicode scalar check.
data Raw = Atom Value | Members [(Text, Raw)] | Elements [Raw]

valueOf :: Raw -> Value
valueOf (Atom v) = v
valueOf (Members xs) = Object (KM.fromList [(K.fromText k, valueOf v) | (k,v) <- xs])
valueOf (Elements xs) = Array (V.fromList (map valueOf xs))

strictJSON :: ByteString -> Either Text Value
strictJSON = fmap valueOf . rawJSON

rawJSON :: ByteString -> Either Text Raw
rawJSON bytes = do
  text <- either (const (Left "invalid UTF-8")) Right (T.decodeUtf8' bytes)
  either (Left . T.pack) Right (A.parseOnly (space *> node <* space <* A.endOfInput) text)

space :: A.Parser ()
space = A.skipWhile (`elem` [' ', '\n', '\r', '\t'])

node :: A.Parser Raw
node = do
  c <- A.peekChar'
  case c of
    '{' -> do
      _ <- A.char '{'; space
      fields <- member `A.sepBy` (space *> A.char ',' <* space)
      space; _ <- A.char '}'
      pure (Members fields)
    '[' -> do
      _ <- A.char '['; space
      values <- node `A.sepBy` (space *> A.char ',' <* space)
      space; _ <- A.char ']'
      pure (Elements values)
    '"' -> Atom . String <$> string
    't' -> A.string "true" *> pure (Atom (Bool True))
    'f' -> A.string "false" *> pure (Atom (Bool False))
    'n' -> A.string "null" *> pure (Atom Null)
    _ -> do
      -- Aeson's Scientific representation retains the exact decimal, including
      -- numbers beyond Double's range, which the profile itself must carry.
      raw <- A.takeWhile1 (`elem` ("-+0123456789.eE" :: String))
      case eitherDecodeStrict' (T.encodeUtf8 raw) of
        Right v@(Number _) -> pure (Atom v)
        _ -> fail "invalid JSON number"
  where member = do
          key <- string; space; _ <- A.char ':'; space
          val <- node
          pure (key, val)

string :: A.Parser Text
string = A.char '"' *> (T.pack <$> many character) <* A.char '"'
  where
    character = do
      c <- A.satisfy (/= '"')
      if c == '\\' then escaped else do
        unless (ord c >= 32 && not (surrogate (ord c))) (fail "invalid JSON scalar")
        pure c
    escaped = do
      c <- A.anyChar
      case c of
        '"' -> pure '"'; '\\' -> pure '\\'; '/' -> pure '/'
        'b' -> pure '\b'; 'f' -> pure '\f'; 'n' -> pure '\n'; 'r' -> pure '\r'; 't' -> pure '\t'
        'u' -> do
          a <- hex4
          if a >= 0xd800 && a <= 0xdbff then do
            _ <- A.string "\\u"
            b <- hex4
            unless (b >= 0xdc00 && b <= 0xdfff) (fail "unpaired surrogate")
            pure (chr (0x10000 + (a-0xd800)*1024 + b-0xdc00))
          else if surrogate a then fail "unpaired surrogate" else pure (chr a)
        _ -> fail "invalid JSON escape"
    hex4 = foldl (\n c -> n*16 + digitToInt c) 0 <$> replicateM 4 (A.satisfy isHexDigit)
    surrogate n = n >= 0xd800 && n <= 0xdfff
