{-# LANGUAGE OverloadedStrings #-}
{-# LANGUAGE ScopedTypeVariables #-}
module Nightseam.Runtime.Identity (DeclarationIdentity(..), identityHandler, checkIdentity) where
import Control.Exception hiding (Handler)
import Control.Monad
import Data.Aeson hiding (Options, defaultOptions)
import qualified Data.Aeson.KeyMap as KM
import Data.Text (Text)
import qualified Data.Text as T
import Nightseam.Runtime

data DeclarationIdentity = DeclarationIdentity { declarationPath :: Text, declarationDigest :: Text } deriving (Eq,Show)
instance ToJSON DeclarationIdentity where
  toJSON (DeclarationIdentity path digest) = object (["path" .= path] ++ if T.null digest then [] else ["digest" .= digest])

invalid :: Text -> IO a
invalid why = throwIO (PublicError "contract_invalid" why Nothing)
valid :: DeclarationIdentity -> IO ()
valid (DeclarationIdentity path digest) = do
  when (T.null path) (invalid "expected a nonempty declaration path")
  unless (T.null digest || T.length digest == 64 && T.all (\c -> c >= '0' && c <= '9' || c >= 'a' && c <= 'f') digest) (invalid "expected lowercase SHA-256 digest")
readIdentity :: Value -> IO DeclarationIdentity
readIdentity (Object members) = do
  unless (all (`elem` ["path","digest"]) (KM.keys members)) (invalid "unknown identity member")
  path <- case KM.lookup "path" members of Just (String p) -> pure p; _ -> invalid "expected path"
  digest <- case KM.lookup "digest" members of Nothing -> pure ""; Just (String d) | not (T.null d) -> pure d; _ -> invalid "invalid digest"
  let identity = DeclarationIdentity path digest
  valid identity
  pure identity
readIdentity _ = invalid "expected identity object"
compareIdentity :: DeclarationIdentity -> DeclarationIdentity -> IO ()
compareIdentity expected actual = when (declarationPath expected /= declarationPath actual || not (T.null (declarationDigest expected)) && not (T.null (declarationDigest actual)) && declarationDigest expected /= declarationDigest actual) $
  throwIO (PublicError "contract_mismatch" ("the declaration identity for " <> declarationPath expected <> " differs") Nothing)
identityHandler :: DeclarationIdentity -> IO Handler
identityHandler expected = do
  valid expected
  pure $ \_ _ value -> do
    actual <- readIdentity value
    compareIdentity expected actual
    pure (toJSON expected)
checkIdentity :: Peer -> CallContext -> DeclarationIdentity -> IO ()
checkIdentity peer ctx expected = do
  valid expected
  (call peer ctx "identity.check" (toJSON expected) >>= readIdentity >>= compareIdentity expected) `catch` \(err :: PublicError) ->
    unless (errorCode err == "method_not_found") (throwIO err)
