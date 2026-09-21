{-# LANGUAGE OverloadedStrings #-}
module Nightseam.Runtime.Validate
  (validate, decodeFrame, strictJSON, validatePattern, matchPattern) where

import Control.Monad (foldM, forM_, unless, when)
import Data.Aeson (Value(..), encode)
import qualified Data.Aeson.Key as K
import qualified Data.Aeson.KeyMap as KM
import Data.ByteString (ByteString)
import qualified Data.ByteString.Lazy as BL
import Data.Char (ord)
import Data.List (sortOn)
import qualified Data.Map.Strict as M
import Data.Maybe (fromMaybe, isJust)
import Data.Scientific (Scientific, coefficient, base10Exponent, formatScientific, FPFormat(Generic), toRealFloat)
import qualified Data.Set as S
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as T
import Data.Time (UTCTime, defaultTimeLocale, parseTimeM)
import qualified Data.Vector as V
import Nightseam.Runtime.Validate.JSON
import Nightseam.Runtime.Validate.Pattern

get :: Text -> Value -> Maybe Value
get k (Object o) = KM.lookup (K.fromText k) o
get _ _ = Nothing
field :: Text -> Value -> Value
field k = fromMaybe Null . get k
text :: Value -> Text
text (String s) = s
text _ = ""
list :: Value -> [Value]
list (Array xs) = V.toList xs
list _ = []
members :: Value -> [(Text,Value)]
members (Object o) = sortOn (utf16 . fst) [(K.toText k,v) | (k,v) <- KM.toList o]
members _ = []
utf16 :: Text -> [Int]
utf16 = concatMap units . T.unpack
  where units c | ord c <= 0xffff = [ord c]
                | otherwise = let n = ord c - 0x10000 in [0xd800 + n `div` 1024,0xdc00 + n `mod` 1024]
isObject :: Value -> Bool
isObject (Object _) = True
isObject _ = False
isString :: Value -> Bool
isString (String _) = True
isString _ = False
bad :: Text -> Text -> Either Text a
bad at want = Left (at <> ": expected " <> want)
fact :: Text -> Text -> Either Text a
fact at why = Left (at <> ": " <> why)
quote :: Text -> Text
quote = T.replace "\x2029" "\\u2029" . T.replace "\x2028" "\\u2028" . T.replace "&" "\\u0026" . T.replace ">" "\\u003e" . T.replace "<" "\\u003c" . T.decodeUtf8 . BL.toStrict . encode

data Context = Context { owner :: Text, schema :: Value, imports :: Value, scope :: M.Map Text Argument }
data Argument = TypeArgument Expression | FamilyArgument Context | UnboundFamily
data Expression = Expression { context :: Context, expression :: Value, ancestry :: S.Set Text }
data Resolved = Resolved { resolved :: Expression, definition :: Maybe Value, typeName :: Text }

child :: Expression -> Value -> Expression
child e v = e { expression = v, ancestry = S.empty }
fresh :: Context -> Value -> Expression
fresh c v = Expression c v S.empty
parameters :: Value -> [Value]
parameters = list . field "parameters"
paramName :: Value -> Text
paramName = text . field "name"
familyParameter :: Value -> Bool
familyParameter = not . T.null . text . field "of"
typeDefinition :: Context -> Text -> Maybe Value
typeDefinition c n = get n (field "types" (schema c))
unbound :: Context -> Text -> Bool
unbound c n = case M.lookup n (scope c) of
  Just UnboundFamily -> True
  Just _ -> False
  Nothing -> any (\p -> paramName p == n && familyParameter p) (parameters (schema c))
singleFamily :: Context -> Maybe Text
singleFamily c = case filter familyParameter (parameters (schema c)) of [p] -> Just (paramName p); _ -> Nothing
importContext :: Context -> Text -> Maybe Context
importContext c n = (\s -> Context n s (imports c) M.empty) <$> get n (imports c)

-- Argument expressions retain their caller's lexical context. Free enclosing
-- family parameters are only those actually reached by the declaration.
freeParameters :: Context -> Value -> [Value]
freeParameters c def = filter ((`S.member` used) . paramName) (parameters (schema c))
  where
    used = walkDefinition S.empty c def
    walkDefinition seen ctx d
      | marker `S.member` seen = S.empty
      | otherwise = S.unions (map (walk (S.insert marker seen) ctx) expressions)
      where
        marker = owner ctx <> ":" <> T.decodeUtf8 (BL.toStrict (encode d))
        bases = [case get "with" b of Just o@(Object _) -> Array (V.fromList (map snd (members o))); _ -> b | b <- list (field "extends" d)]
        expressions = field "type" d : map (field "type") (list (field "fields" d)) ++ bases ++ map snd (members (field "variants" d))
    walk seen ctx (String n) =
      let (prefix,suffix) = T.breakOn "." n
      in if any ((==prefix) . paramName) (parameters (schema ctx)) then S.singleton prefix
         else if T.null suffix then maybe S.empty (walkDefinition seen ctx) (typeDefinition ctx n)
         else case importContext ctx prefix of
           Nothing -> S.empty
           Just target -> case typeDefinition target (T.drop 1 suffix) of
             Nothing -> S.empty
             Just d -> if any familyParameter (filter ((`S.member` walkDefinition seen target d) . paramName) (parameters (schema target)) ++ parameters d)
               then maybe S.empty S.singleton (singleFamily ctx) else S.empty
    walk seen ctx o@(Object _) = case firstPresent ["array","map","nullable"] o of
      Just x -> walk seen ctx x
      Nothing -> case get "apply" o of
        Just (String n) -> S.union
          (if "." `T.isInfixOf` n then S.empty else maybe S.empty (walkDefinition seen ctx) (typeDefinition ctx n))
          (S.unions [walk seen ctx v | (_,v) <- members (field "with" o)])
        _ -> case get "ref" o of
          Just (String n) -> case typeDefinition ctx n of
            Nothing -> S.empty
            Just d -> S.unions [walk seen ctx (field "type" f) | f <- list (field "fields" d), field "name" f == field "key" d]
          _ | isJust (get "kind" o) -> walkDefinition seen ctx o
            | otherwise -> S.empty
    walk seen ctx (Array xs) = S.unions (map (walk seen ctx) (V.toList xs))
    walk _ _ _ = S.empty

firstPresent :: [Text] -> Value -> Maybe Value
firstPresent [] _ = Nothing
firstPresent (k:ks) v = case get k v of Just x -> Just x; _ -> firstPresent ks v

named :: Expression -> Text -> Text -> Either Text (Expression,Value,Text)
named e name at = do
  let (family,suffix) = T.breakOn "." name
      caller = context e
  (target,member) <- if T.null suffix then Right (e,name) else do
    when (T.null family) (bad at "known family")
    ctx <- if upper family then case M.lookup family (scope caller) of
      Just (FamilyArgument c) -> Right c
      _ -> bad at ("a binding of the parameter " <> family)
      else maybe (bad at "known family") Right (importContext caller family)
    let member = T.drop 1 suffix
        needed = maybe [] (\d -> freeParameters ctx d ++ parameters d) (typeDefinition ctx member)
        inheritedScope = case singleFamily caller of
          Just source | not (upper family) ->
            let argument = case M.lookup source (scope caller) of
                  Just a -> Just a
                  Nothing | unbound caller source -> Just UnboundFamily
                  _ -> Nothing
            in case argument of
              Just a -> foldr (\p -> M.insert (paramName p) a) (scope ctx) (filter familyParameter needed)
              _ -> scope ctx
          _ -> scope ctx
    pure (fresh (ctx {scope = inheritedScope}) (String member),member)
  d <- maybe (bad at "known type") Right (typeDefinition (context target) member)
  pure (target {expression = String member}, d, member)
  where upper t = case T.uncons t of Just (c,_) -> c >= 'A' && c <= 'Z'; _ -> False

resolve :: Bool -> Expression -> Text -> Either Text Resolved
resolve inheritance e at = loop inheritance (ancestry e) e
  where
    loop inBase seen current = case expression current of
      String n -> case M.lookup n (scope (context current)) of
        Just (TypeArgument supplied) -> loop inBase (ancestry supplied) supplied
        Just _ -> bad at "type argument"
        Nothing
          | let (family,suffix) = T.breakOn "." n, not (T.null suffix) && unbound (context current) family -> pure (Resolved (child current (String "json")) Nothing "")
          | n `elem` ["json","string","boolean","number","integer","timestamp"] -> pure (Resolved current Nothing "")
          | otherwise -> do (target,d,name) <- named current n at; finish seen target d name
      o@(Object _) -> case get "apply" o of
        Just (String n) -> do
          (target,d,name) <- named current n at
          fillers <- case get "with" o of Just f@(Object _) -> Right f; _ -> bad at "application arguments"
          let params = (if inBase || "." `T.isInfixOf` n then freeParameters (context target) d else []) ++ parameters d
          arguments <- foldM (bind current seen fillers) (scope (context target)) params
          forM_ (members fillers) $ \(key,_) -> unless (key `elem` map paramName params) (bad at ("known parameter " <> key))
          finish seen (target {context = (context target) {scope = arguments}}) d name
        _ | isJust (get "kind" o) -> finish seen current o (text (field "kind" o))
          | otherwise -> pure (Resolved current Nothing "")
      _ -> bad at "type expression"
    finish seen target d name
      | field "kind" d /= String "alias" = pure (Resolved target (Just d) name)
      | marker `S.member` seen = bad at "acyclic type expression"
      | otherwise = loop False (S.insert marker seen) (target {expression = field "type" d})
      where marker = definitionKey target name d
    bind current seen fillers acc p = do
      let n = paramName p
      filler <- maybe (bad at ("an argument for " <> n)) Right (get n fillers)
      argument <- if not (familyParameter p) then Right (TypeArgument ((child current filler) {ancestry = seen})) else case filler of
        String family | unbound (context current) family -> Right UnboundFamily
        String family -> case M.lookup family (scope (context current)) of
          Just (FamilyArgument ctx) -> Right (FamilyArgument ctx)
          Just _ -> bad at ("known family argument for " <> n)
          Nothing -> maybe (bad at ("known family argument for " <> n)) (Right . FamilyArgument) (importContext (context current) family)
        _ -> bad at ("family argument for " <> n)
      pure (M.insert n argument acc)

inherited :: Expression -> Value -> Text -> Either Text Resolved
inherited e base at = do
  case base of
    String n -> do
      (target,d,_) <- named e n at
      unless (null (parameters d) && null (freeParameters (context target) d)) (bad at ("explicit application of generic base " <> n))
    _ -> pure ()
  resolve True (child e base) at

definitionKey :: Expression -> Text -> Value -> Text
definitionKey e name d = owner (context e) <> ":" <> name <> ":" <> T.decodeUtf8 (BL.toStrict (encode d))

fields :: Resolved -> Text -> S.Set Text -> Either Text [(Value,Expression)]
fields r at seen = case definition r of
  Nothing -> bad at "record"
  Just d -> do
    let marker = definitionKey (resolved r) (typeName r) d
    when (marker `S.member` seen) (bad at "acyclic inheritance")
    parents <- mapM (\base -> inherited (resolved r) base at >>= \parent -> fields parent at (S.insert marker seen)) (list (field "extends" d))
    pure (concat parents ++ [(f,child (resolved r) (field "type" f)) | f <- list (field "fields" d)])

variants :: Resolved -> Text -> S.Set Text -> Either Text (M.Map Text Expression)
variants r at seen = case definition r of
  Just d | field "kind" d == String "union" -> do
    let marker = definitionKey (resolved r) (typeName r) d
    when (marker `S.member` seen) (bad at "acyclic inheritance")
    parents <- mapM (\base -> inherited (resolved r) base at >>= \parent -> variants parent at (S.insert marker seen)) (list (field "extends" d))
    pure (M.unions (M.fromList [(k,child (resolved r) v) | (k,v) <- members (field "variants" d)] : reverse parents))
  _ -> bad at "union"

checkPatterns :: Value -> Either Text ()
checkPatterns v = do
  case get "pattern" v of
    Just (String p) -> case validatePattern p of Left _ -> Left ("pattern " <> quote p <> ": outside Nightseam dialect"); _ -> pure ()
    _ -> pure ()
  case v of
    Object _ -> mapM_ (checkPatterns . snd) (members v)
    Array xs -> mapM_ checkPatterns xs
    _ -> pure ()

-- Bindings use the shared table's {name: {type: expression} | {family: name}}
-- form. A directly supplied expression is accepted as a type binding too.
validate :: Value -> Value -> Value -> Value -> Value -> Either Text ()
validate description imported bindings expr value = do
  unless (isObject (field "types" description)) (Left "expected family descriptor with types")
  mapM_ checkPatterns [description, imported, bindings, expr]
  let original = Context "$" description imported M.empty
  bound <- foldM (bind original) M.empty (members bindings)
  let draws = M.fromListWith M.union
        [(family,M.singleton (T.drop 1 suffix) argument)
        | (name,argument@(TypeArgument _)) <- M.toList bound
        , let (family,suffix) = T.breakOn "." name
        , not (T.null family), T.length suffix > 1]
      drawnFamily family arguments = FamilyArgument (Context ("$draw:" <> family)
        (Object (KM.singleton "types" (Object (KM.fromList
          [(K.fromText name,Object (KM.fromList [("kind",String "alias"),("type",String name)])) | name <- M.keys arguments]))))
        imported arguments)
      withDraws = M.foldrWithKey (\family arguments current -> case M.lookup family bound of
        Just (FamilyArgument _) -> current
        _ -> M.insert family (drawnFamily family arguments) current) bound draws
  validateAt (fresh (original {scope = withDraws}) expr) value "$"
  where
    bind original acc (name,slot) = case get "family" slot of
      Just (String family) -> case importContext original family of
        Just c -> Right (M.insert name (FamilyArgument c) acc)
        _ -> bad "$" ("known family argument for " <> name)
      _ -> Right (M.insert name (TypeArgument (fresh original (fromMaybe slot (get "type" slot)))) acc)

validateAt :: Expression -> Value -> Text -> Either Text ()
validateAt e value at = do
  r <- resolve False e at
  case definition r of
    Just d -> case text (field "kind" d) of
      "enum" -> unless (value `elem` list (field "values" d)) (bad at (typeName r))
      "record" -> record r d
      "entity" -> record r d
      "union" -> union r d
      "callable" -> callable d
      _ -> bad at "supported type"
    Nothing -> case expression (resolved r) of
      o@(Object _) -> composite r o
      String n -> primitive n value at
      _ -> bad at "type expression"
  where
    record r d = do
      unless (isObject value) (bad at (typeName r <> " object"))
      fs <- fields r at S.empty
      forM_ fs $ \(f,fe) -> do
        let name = text (field "name" f); location = at <> "." <> name
        case get name value of
          Nothing -> when (field "required" f == Bool True) (fact location "required field missing")
          Just Null | field "nullable" f == Bool True -> pure ()
          Just item -> do
            when (item == Null) $ do
              nullable <- resolve False fe location
              unless (isJust (get "nullable" (expression (resolved nullable)))) (fact location "null is not permitted")
            validateAt fe item location
            when (item /= Null) (constrain f item location)
      forM_ (members value) $ \(name,item) -> unless (name `elem` map (text . field "name" . fst) fs) $
        if field "open" d == Bool True then validateJSON item (at <> "." <> name) else fact (at <> "." <> name) "unknown field"
    union r d = do
      unless (isObject value) (bad at (typeName r <> " object"))
      let tag = text (field "tag" d); member = case text (field "value" d) of "" -> "value"; n -> n
      t <- maybe (fact (at <> "." <> tag) "required field missing") Right (get tag value)
      vs <- variants r at S.empty
      variant <- case t of String s -> maybe (bad (at <> "." <> tag) "known variant") Right (M.lookup s vs); _ -> bad (at <> "." <> tag) "known variant"
      let empty = members (expression variant) == [("empty",Bool True)]
      unless empty $ do
        v <- maybe (fact (at <> "." <> member) "required field missing") Right (get member value)
        validateAt variant v (at <> "." <> member)
      forM_ (members value) $ \(key,_) -> unless (key == tag || not empty && key == member) (fact (at <> "." <> key) "unknown field")
    callable d = do
      let contract = text (field "contract" d)
      unless (isObject value) (bad at ("a live reference to " <> contract))
      when (T.null (text (field "binding" value))) (fact (at <> ".binding") "a live reference names the binding it refers to")
      actual <- case get "contract" value of Just (String s) -> pure s; _ -> fact (at <> ".contract") "a live reference carries the declaration it implements"
      unless (actual == contract) (fact (at <> ".contract") ("the reference carries " <> actual <> " where " <> contract <> " is expected"))
      case get "digest" value of Just digest -> unless (validDigest digest) (bad (at <> ".digest") "lowercase SHA-256 digest"); _ -> pure ()
      forM_ (members value) $ \(key,_) -> unless (key `elem` ["binding","contract","digest"]) (fact (at <> "." <> key) "unknown field")
    composite r o
      | Just inner <- get "nullable" o = unless (value == Null) (validateAt (child (resolved r) inner) value at)
      | Just literal <- get "literal" o = do
          unless (isString literal && not (T.null (text literal))) (bad at "nonempty string literal")
          unless (value == literal) (bad at ("literal " <> quote (text literal)))
      | Just element <- get "array" o = case value of
          Array xs -> forM_ (zip [0::Int ..] (V.toList xs)) $ \(i,item) -> validateAt (child (resolved r) element) item (index at i)
          _ -> bad at "array"
      | Just element <- get "map" o = do
          unless (isObject value) (bad at "object")
          forM_ (members value) $ \(key,item) -> validateAt (child (resolved r) element) item (at <> "." <> key)
      | Just (String entity) <- get "ref" o = do
          (target,d,_) <- case named (resolved r) entity at of Left _ -> bad at "known entity"; Right x -> Right x
          found <- resolve False target at >>= \rr -> fields rr at S.empty
          case [fe | (f,fe) <- found, field "name" f == field "key" d] of
            fe:_ -> validateAt fe value at
            _ -> bad at "entity with a key"
      | isJust (get "empty" o) = unless (isObject value && null (members value)) (bad at "empty object")
      | otherwise = bad at "supported type expression"

index :: Text -> Int -> Text
index at i = at <> "[" <> T.pack (show i) <> "]"
finite :: Scientific -> Bool
finite n = let d = toRealFloat n :: Double in not (isInfinite d || isNaN d)
safeInteger :: Scientific -> Bool
safeInteger n
  | c == 0 = True
  | power >= 0 = power <= 16 && length digits + power <= 16 && c * 10^power <= 9007199254740991
  | negate (toInteger power) > toInteger (length digits) = False
  | otherwise = let divisor = 10 ^ negate power in c `mod` divisor == 0 && c `div` divisor <= 9007199254740991
  where c = abs (coefficient n); power = base10Exponent n; digits = show c

primitive :: Text -> Value -> Text -> Either Text ()
primitive "json" v at = validateJSON v at
primitive "string" v at = unless (isString v) (bad at "string")
primitive "boolean" (Bool _) _ = pure ()
primitive "boolean" _ at = bad at "boolean"
primitive n v at | n == "number" || n == "integer" = case v of
  Number number -> do
    unless (finite number) (bad at "finite number")
    when (n == "integer" && not (safeInteger number)) (bad at "JavaScript-safe integer")
  _ -> bad at n
primitive "timestamp" (String s) at = unless (timestamp s) (bad at "RFC3339 timestamp")
primitive "timestamp" _ at = bad at "timestamp"
primitive _ _ at = bad at "known type"

timestamp :: Text -> Bool
timestamp s = matchPattern "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:[.,][0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})$" s
  && isJust (parseTimeM True defaultTimeLocale "%Y-%m-%dT%H:%M:%S%Q%Ez" (T.unpack normalized) :: Maybe UTCTime)
  && T.take 2 (T.drop 17 s) < "60"
  where normalized = T.replace "Z" "+00:00" (T.replace "," "." s)

validateJSON :: Value -> Text -> Either Text ()
validateJSON (Number n) at = unless (finite n) (bad at "finite JSON number")
validateJSON (Array xs) at = forM_ (zip [0::Int ..] (V.toList xs)) $ \(i,x) -> validateJSON x (index at i)
validateJSON o@(Object _) at = forM_ (members o) $ \(k,x) -> validateJSON x (at <> "." <> k)
validateJSON _ _ = pure ()

numberText :: Value -> Text
numberText (Number n) | base10Exponent n >= 0 && base10Exponent n <= 16 = T.pack (show (coefficient n * 10 ^ base10Exponent n))
numberText (Number n) = T.pack (formatScientific Generic Nothing n)
numberText (String s) = s
numberText _ = ""
constrain :: Value -> Value -> Text -> Either Text ()
constrain f value at = do
  case value of
    Number n -> forM_ [("min",(<),"at least "),("max",(>),"at most ")] $ \(key,cmp,label) ->
      case get key f of Just limit@(Number b) -> when ((toRealFloat n :: Double) `cmp` toRealFloat b) (bad at (label <> numberText limit)); _ -> pure ()
    String s -> forM_ [("min",(<),"at or after "),("max",(>),"at or before ")] $ \(key,cmp,label) ->
      case get key f of Just limit -> when (s `cmp` numberText limit) (bad at (label <> numberText limit)); _ -> pure ()
    _ -> pure ()
  let len = case value of String s -> Just (length (T.unpack s)); Array xs -> Just (V.length xs); _ -> Nothing
  forM_ len $ \n -> forM_ [("min",(<),"at least "),("max",(>),"at most ")] $ \(key,cmp,label) ->
    case get key (field "length" f) of
      Just limit@(Number b) -> when (fromIntegral n `cmp` b) (bad at ("a length of " <> label <> numberText limit))
      _ -> pure ()
  case (get "pattern" f,value) of
    (Just (String p),String s) -> unless (T.null p || matchPattern p s) (bad at ("a match of " <> p))
    _ -> pure ()

validDigest :: Value -> Bool
validDigest (String s) = T.length s == 64 && T.all lowerHex s
validDigest _ = False
lowerHex :: Char -> Bool
lowerHex c = c >= '0' && c <= '9' || c >= 'a' && c <= 'f'

-- The role is the receiver's role. Payload numbers are deliberately unchecked:
-- representability belongs to descriptor validation, not the envelope.
decodeFrame :: Text -> ByteString -> Either Text Value
decodeFrame role bytes = do
  raw <- rawJSON bytes
  pairs <- case raw of Members ps -> Right ps; _ -> Left "duplex frame must be an object"
  unless (length pairs == S.size (S.fromList (map fst pairs))) (Left "duplicate duplex field")
  let value = valueOf raw
      present k = isJust (get k value)
      string k = case get k value of Just (String s) -> not (T.null s); _ -> False
      kind = text (field "kind" value)
      allowed = ["version","kind","traceparent","tracestate"] ++ case kind of
        "request" -> ["id","method","params","meta"]
        "response" -> ["id","result","error"]
        "event" -> ["event","data","meta"]
        "cancel" -> ["id"]
        _ -> []
      shape = case kind of
        "request" -> string "id" && string "method" && present "params"
        "response" -> string "id" && present "result" /= present "error"
        "event" -> string "event" && present "data"
        "cancel" -> string "id"
        _ -> False
  unless (field "version" value == Number 1 && shape && all ((`elem` allowed) . fst) pairs) (Left "invalid duplex frame shape")
  when (kind /= "event") $ do
    let local = if role == "server" then "s:" else "c:"
        remote = if role == "server" then "c:" else "s:"
        prefix = if kind == "response" then local else remote
        ident = text (field "id" value)
        digits = T.drop 2 ident
    unless (prefix `T.isPrefixOf` ident && not (T.null digits) && T.length digits <= 20 && T.head digits /= '0' && T.all (\c -> c >= '0' && c <= '9') digits) (Left "invalid duplex request identifier")
  case get "error" value of
    Nothing -> pure ()
    Just err -> unless (isObject err && all ((`elem` ["code","message","data"]) . fst) (members err)
      && not (T.null (text (field "code" err))) && not (T.null (text (field "message" err)))) (Left "invalid duplex error")
  case get "traceparent" value of
    Nothing -> pure ()
    Just (String s) -> unless (traceparent s) (Left "invalid traceparent")
    _ -> Left "invalid traceparent"
  case get "tracestate" value of Nothing -> pure (); Just (String _) -> pure (); _ -> Left "invalid tracestate"
  case get "meta" value of
    Nothing -> pure ()
    Just meta -> unless (isObject meta && all (\(k,v) -> not ("nightseam." `T.isPrefixOf` k) && isString v) (members meta)) (Left "invalid meta")
  pure value

traceparent :: Text -> Bool
traceparent s = case T.splitOn "-" s of
  [version,trace,spanId,flags] -> and [T.length version == 2, T.length trace == 32,
    T.length spanId == 16, T.length flags == 2,
    all (T.all lowerHex) [version,trace,spanId,flags]]
  _ -> False
