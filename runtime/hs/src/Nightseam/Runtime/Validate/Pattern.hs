{-# LANGUAGE OverloadedStrings #-}
module Nightseam.Runtime.Validate.Pattern (validatePattern, matchPattern) where

import Data.Char (digitToInt, isHexDigit, ord)
import Data.List (sortOn)
import qualified Data.IntSet as S
import qualified Data.Map.Lazy as M
import Data.Text (Text)
import qualified Data.Text as T

type Chars = [(Int,Int)]
data RE = Set Chars | Assert Char | Seq [RE] | Alt [RE] | Repeat RE Integer (Maybe Integer)
  deriving (Eq, Ord, Show)
type Parsed a = Either () (a,String)

validatePattern :: Text -> Either Text ()
validatePattern text = case parse text of
  Left () -> Left "outside Nightseam dialect"
  Right _ -> Right ()

parse :: Text -> Either () RE
parse text = do
  (r,rest) <- disjunction (T.unpack text)
  if null rest then Right r else Left ()

disjunction :: String -> Parsed RE
disjunction input = do
  (r,rest) <- sequenceRE input
  case rest of
    '|':more -> do (rs,end) <- disjunction more; pure (Alt [r,rs],end)
    _ -> pure (r,rest)

sequenceRE :: String -> Parsed RE
sequenceRE input = go [] input
  where
    go acc [] = Right (Seq (reverse acc),[])
    go acc rest@(c:_) | c == ')' || c == '|' = Right (Seq (reverse acc),rest)
    go acc rest = do
      (r,tail') <- atom rest
      (q,end) <- quantifier r tail'
      go (q:acc) end

atom :: String -> Parsed RE
atom [] = Left ()
atom ('^':xs) = Right (Assert '^',xs)
atom ('$':xs) = Right (Assert '$',xs)
atom ('.':xs) = Right (Set (complement [(10,10),(13,13),(0x2028,0x2029)]),xs)
atom ('(':xs) = do
  more <- case xs of '?':':':ys -> Right ys; '?':_ -> Left (); _ -> Right xs
  (r,end) <- disjunction more
  case end of ')':ys -> Right (r,ys); _ -> Left ()
atom ('[':'[':':':_) = Left ()
atom ('[':xs) = do (s,end) <- charClass xs; pure (Set s,end)
atom ('\\':xs) = escape False xs
atom (c:xs) | c `elem` ("*+?{}]" :: String) = Left ()
            | otherwise = Right (Set [(ord c,ord c)],xs)

quantifier :: RE -> String -> Parsed RE
quantifier r input = case input of
  '*':xs -> finish 0 Nothing xs
  '+':xs -> finish 1 Nothing xs
  '?':xs -> finish 0 (Just 1) xs
  '{':xs -> do
    (lo,rest) <- decimal xs
    case rest of
      '}':end -> finish lo (Just lo) end
      ',':'}':end -> finish lo Nothing end
      ',':ys -> do
        (hi,tail') <- decimal ys
        case tail' of '}':end | hi >= lo -> finish lo (Just hi) end; _ -> Left ()
      _ -> Left ()
  _ -> Right (r,input)
  where
    finish lo hi rest = case r of
      Assert _ -> Left ()
      _ -> Right (Repeat r lo hi, case rest of '?':xs -> xs; _ -> rest)
    decimal xs = let (numerals,rest) = span asciiDigit xs in
      if null numerals then Left () else Right (read numerals :: Integer,rest)

charClass :: String -> Parsed Chars
charClass input = case input of
  '^':xs -> do (cs,rest) <- go [] xs; pure (complement cs,rest)
  _ -> go [] input
  where
    go acc (']':xs) = Right (normalize acc,xs)
    go _ [] = Left ()
    go acc xs = do
      (a,rest) <- classAtom xs
      case rest of
        '-':end@(c:_) | c /= ']' -> do
          (b,tail') <- classAtom end
          case (a,b) of
            ([(lo,lo')],[(hi,hi')]) | lo == lo' && hi == hi' && lo <= hi -> go ((lo,hi):acc) tail'
            _ -> Left ()
        _ -> go (a ++ acc) rest
    classAtom ('\\':xs) = do
      (r,rest) <- escape True xs
      case r of Set cs -> Right (cs,rest); _ -> Left ()
    classAtom (c:xs) = Right ([(ord c,ord c)],xs)
    classAtom [] = Left ()

escape :: Bool -> String -> Parsed RE
escape _ [] = Left ()
escape cls (c:xs) = case c of
  'd' -> chars digits xs; 'D' -> chars (complement digits) xs
  'w' -> chars wordsSet xs; 'W' -> chars (complement wordsSet) xs
  's' -> chars whitespace xs; 'S' -> chars (complement whitespace) xs
  'b' | not cls -> Right (Assert 'b',xs)
  'B' | not cls -> Right (Assert 'B',xs)
  'b' -> single 8 xs
  'f' -> single 12 xs; 'n' -> single 10 xs; 'r' -> single 13 xs
  't' -> single 9 xs; 'v' -> single 11 xs
  '0' -> if not (null xs) && asciiDigit (head xs) then Left () else single 0 xs
  'c' -> case xs of a:rest | asciiLetter a -> single (ord a `mod` 32) rest; _ -> Left ()
  'x' -> do (n,rest) <- hexadecimal 2 xs; single n rest
  'u' -> case xs of
    '{':rest -> let (hex,end) = span isHexDigit rest
                    n = foldl (\a h -> a*16 + toInteger (digitToInt h)) 0 hex
                in case end of '}':tail' | not (null hex) && n <= 0x10ffff -> single (fromInteger n) tail'; _ -> Left ()
    _ -> do
      (n,rest) <- hexadecimal 4 xs
      if n >= 0xd800 && n <= 0xdbff then case rest of
        '\\':'u':more -> case hexadecimal 4 more of
          Right (m,end) | m >= 0xdc00 && m <= 0xdfff -> single (0x10000 + (n-0xd800)*1024 + m-0xdc00) end
          _ -> single n rest
        _ -> single n rest
      else single n rest
  _ | c `elem` ("^$\\.*+?()[]{}|/" :: String) || (cls && c == '-') -> single (ord c) xs
    | otherwise -> Left ()
  where
    chars cs rest = Right (Set cs,rest)
    single n = chars [(n,n)]

hexadecimal :: Int -> String -> Parsed Int
hexadecimal n xs = let (h,rest) = splitAt n xs in
  if length h == n && all isHexDigit h then Right (foldl (\v c -> v*16 + digitToInt c) 0 h,rest) else Left ()

asciiDigit :: Char -> Bool
asciiDigit c = c >= '0' && c <= '9'
asciiLetter :: Char -> Bool
asciiLetter c = c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
digits, wordsSet, whitespace :: Chars
digits = [(48,57)]
wordsSet = [(48,57),(65,90),(95,95),(97,122)]
whitespace = [(9,13),(32,32),(160,160),(0x1680,0x1680),(0x2000,0x200a),(0x2028,0x2029),(0x202f,0x202f),(0x205f,0x205f),(0x3000,0x3000),(0xfeff,0xfeff)]

normalize :: Chars -> Chars
normalize = foldr add [] . sortOn fst
  where add (lo,hi) ((a,b):xs) | hi+1 >= a = (lo,max hi b):xs
        add p xs = p:xs
complement :: Chars -> Chars
complement cs = go 0 (normalize cs)
  where go n [] = [(n,0x10ffff) | n <= 0x10ffff]
        go n ((lo,hi):xs) = [(n,lo-1) | n < lo] ++ go (hi+1) xs

width :: RE -> Integer
width (Set _) = 1
width (Assert _) = 0
width (Seq rs) = sum (map width rs)
width (Alt rs) = minimum (map width rs)
width (Repeat r n _) = n * width r

-- Memoized sets of ending offsets avoid backtracking and repeated expansion.
-- A large counted repetition is never expanded; nullable fixed points settle
-- immediately and minimum widths reject counts longer than the input.
matchPattern :: Text -> Text -> Bool
matchPattern source text = case parse source of
  Left () -> False
  Right root -> let
    input = T.unpack text
    size = length input
    nodes r = r : case r of Seq rs -> concatMap nodes rs; Alt rs -> concatMap nodes rs; Repeat child _ _ -> nodes child; _ -> []
    memo = M.fromList [((r,start), compute r start) | r <- nodes root, start <- [0..size]]
    ends r start = M.findWithDefault S.empty (r,start) memo
    advance r starts = S.unions [ends r start | start <- S.toList starts]
    wordAt i = i >= 0 && i < size && let c = input !! i in asciiDigit c || asciiLetter c || c == '_'
    compute r start
      | width r > toInteger (size-start) = S.empty
      | otherwise = case r of
          Set cs -> if start < size && any (\(lo,hi) -> let c = ord (input !! start) in c >= lo && c <= hi) cs then S.singleton (start+1) else S.empty
          Assert c -> if case c of '^' -> start == 0; '$' -> start == size; 'b' -> wordAt (start-1) /= wordAt start; _ -> wordAt (start-1) == wordAt start then S.singleton start else S.empty
          Seq rs -> foldl (flip advance) (S.singleton start) rs
          Alt rs -> S.unions [ends child start | child <- rs]
          Repeat child lo hi -> let
            required n current
              | S.null current = S.empty
              | n >= lo = optional n current current
              | otherwise = let next = advance child current in
                  if next == current then optional lo current current else required (n+1) next
            optional n current result
              | maybe False (n >=) hi = result
              | otherwise = let next = advance child current in
                  if S.null next || next == current then result else optional (n+1) next (S.union result next)
            in required 0 (S.singleton start)
    in any (not . S.null . ends root) [0..size]
