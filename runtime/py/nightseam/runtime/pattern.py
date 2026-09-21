"""The regular subset of ECMAScript Unicode patterns, over scalar code points.

Parsing and matching follow the reference's internal/pattern implementation.
Counted repetition is evaluated as sets of positions, so even enormous nullable
counts converge without expanding the expression or narrowing its domain.
"""

from dataclasses import dataclass
from functools import cache, lru_cache

from .json import dumps

MAX_CODEPOINT = 0x10FFFF
DIGITS = ((48, 57),)
WORDS = ((48, 57), (65, 90), (95, 95), (97, 122))
SPACE = (
    (9, 13),
    (32, 32),
    (160, 160),
    (0x1680, 0x1680),
    (0x2000, 0x200A),
    (0x2028, 0x2029),
    (0x202F, 0x202F),
    (0x205F, 0x205F),
    (0x3000, 0x3000),
    (0xFEFF, 0xFEFF),
)
OUTSIDE = (
    "(?P<",
    "(?P=",
    "(?<=",
    "(?<!",
    "(?<",
    "(?=",
    "(?!",
    "[[:",
    "\\k<",
    "\\p{",
    "\\P{",
    "\\A",
    "\\z",
    "\\Z",
    "\\Q",
    "\\C",
)


def normalized(spans):
    result = []
    for low, high in sorted(spans):
        if result and low <= result[-1][1] + 1:
            result[-1] = (result[-1][0], max(result[-1][1], high))
        else:
            result.append((low, high))
    return tuple(result)


def complement(spans):
    result, start = [], 0
    for low, high in normalized(spans):
        if start < low:
            result.append((start, low - 1))
        start = high + 1
    if start <= MAX_CODEPOINT:
        result.append((start, MAX_CODEPOINT))
    return tuple(result)


@dataclass(frozen=True)
class Node:
    kind: str
    spans: tuple = ()
    children: tuple = ()
    minimum: int = 0
    maximum: int | None = 0
    width: int = 0


def character(spans):
    return Node("c", spans=normalized(spans), width=1)


def join(kind, children):
    if len(children) == 1:
        return children[0]
    widths = [node.width for node in children]
    return Node(kind, children=tuple(children), width=min(widths, default=0) if kind == "|" else sum(widths))


class Parser:
    def __init__(self, source):
        self.source, self.at = source, 0

    def fail(self):
        raise ValueError("pattern " + dumps(self.source) + ": outside Nightseam dialect")

    def peek(self):
        return self.source[self.at : self.at + 1]

    def take(self):
        result = self.peek()
        if not result:
            self.fail()
        self.at += 1
        return result

    def disjunction(self, group=False):
        branches, sequence = [], []
        while self.peek():
            if self.peek() == ")":
                if not group:
                    self.fail()
                self.at += 1
                return join("|", branches + [join("s", sequence)])
            if self.peek() == "|":
                self.at += 1
                branches.append(join("s", sequence))
                sequence = []
                continue
            node, assertion = self.atom()
            quantifier = self.peek()
            if quantifier and quantifier in "*+?{":
                self.at += 1
                if assertion:
                    self.fail()
                if quantifier == "{":
                    minimum = self.decimal()
                    maximum = minimum
                    if self.peek() == ",":
                        self.at += 1
                        maximum = self.decimal() if self.peek() in "0123456789" and self.peek() else None
                    if self.take() != "}" or maximum is not None and minimum > maximum:
                        self.fail()
                else:
                    minimum = int(quantifier == "+")
                    maximum = 1 if quantifier == "?" else None
                if self.peek() == "?":
                    self.at += 1
                node = Node("r", children=(node,), minimum=minimum, maximum=maximum, width=node.width * minimum)
            sequence.append(node)
        if group:
            self.fail()
        return join("|", branches + [join("s", sequence)])

    def decimal(self):
        start = self.at
        while self.peek() and self.peek() in "0123456789":
            self.at += 1
        if start == self.at:
            self.fail()
        # Python integers retain ordering even beyond engine repetition limits.
        value = 0
        for digit in self.source[start : self.at]:
            value = value * 10 + ord(digit) - 48
        return value

    def atom(self):
        if self.source.startswith(OUTSIDE, self.at):
            self.fail()
        char = self.take()
        if char in "^$":
            return Node(char), True
        if char == ".":
            return character(complement(((10, 10), (13, 13), (0x2028, 0x2029)))), False
        if char == "(":
            if self.peek() == "?":
                if not self.source.startswith("?:", self.at):
                    self.fail()
                self.at += 2
            return self.disjunction(True), False
        if char == "[":
            return character(self.character_class()), False
        if char == "\\":
            spans, assertion = self.escape(False)
            return (Node(assertion), True) if assertion else (character(spans), False)
        if char in "*+?{}]":
            self.fail()
        return character(((ord(char), ord(char)),)), False

    def character_class(self):
        negated = self.peek() == "^"
        if negated:
            self.at += 1
        spans = []
        while self.peek() and self.peek() != "]":
            left = self.class_atom()
            if self.peek() == "-" and self.at + 1 < len(self.source) and self.source[self.at + 1] != "]":
                self.at += 1
                right = self.class_atom()
                if (
                    len(left) != 1
                    or len(right) != 1
                    or left[0][0] != left[0][1]
                    or right[0][0] != right[0][1]
                    or left[0][0] > right[0][0]
                ):
                    self.fail()
                spans.append((left[0][0], right[0][0]))
            else:
                spans.extend(left)
        if self.take() != "]":
            self.fail()
        return complement(spans) if negated else normalized(spans)

    def class_atom(self):
        char = self.take()
        return self.escape(True)[0] if char == "\\" else ((ord(char), ord(char)),)

    def hex(self, count):
        text = self.source[self.at : self.at + count]
        if len(text) != count or any(char not in "0123456789abcdefABCDEF" for char in text):
            self.fail()
        self.at += count
        return int(text, 16)

    def escape(self, in_class):
        if self.source.startswith(OUTSIDE, self.at - 1):
            self.fail()
        char = self.take()
        classes = {"d": DIGITS, "w": WORDS, "s": SPACE}
        if char.lower() in classes:
            spans = classes[char.lower()]
            return (complement(spans) if char.isupper() else spans), None
        if char in "bB" and not in_class:
            return (), char
        escapes = {"f": 12, "n": 10, "r": 13, "t": 9, "v": 11}
        if char in escapes:
            code = escapes[char]
        elif char == "b" and in_class:
            code = 8
        elif char == "0":
            if self.peek() and self.peek() in "0123456789":
                self.fail()
            code = 0
        elif char == "c":
            letter = self.take()
            if not ("A" <= letter <= "Z" or "a" <= letter <= "z"):
                self.fail()
            code = ord(letter) & 31
        elif char == "x":
            code = self.hex(2)
        elif char == "u":
            if self.peek() == "{":
                self.at += 1
                start = self.at
                while self.peek() and self.peek() in "0123456789abcdefABCDEF":
                    self.at += 1
                text = self.source[start : self.at]
                if not text or self.take() != "}":
                    self.fail()
                code = int(text, 16)
                if code > MAX_CODEPOINT:
                    self.fail()
            else:
                code = self.hex(4)
                if 0xD800 <= code <= 0xDBFF and self.source.startswith("\\u", self.at):
                    end = self.at
                    self.at += 2
                    try:
                        low = self.hex(4)
                    except ValueError:
                        low = -1
                    if 0xDC00 <= low <= 0xDFFF:
                        code = 0x10000 + ((code - 0xD800) << 10) + low - 0xDC00
                    else:
                        self.at = end
        elif char in "^$\\.*+?()[]{}|/" or in_class and char == "-":
            code = ord(char)
        else:
            self.fail()
        return ((code, code),), None


class Pattern:
    def __init__(self, source):
        self.tree = Parser(source).disjunction()

    def matches(self, value):
        def word(at):
            return 0 <= at < len(value) and any(low <= ord(value[at]) <= high for low, high in WORDS)

        def advance(child, positions):
            return frozenset(end for start in positions for end in ends(child, start))

        @cache
        def ends(node, start):
            if node.width > len(value) - start:
                return frozenset()
            kind = node.kind
            if kind == "c":
                return (
                    frozenset((start + 1,))
                    if start < len(value) and any(low <= ord(value[start]) <= high for low, high in node.spans)
                    else frozenset()
                )
            if kind in "^$bB":
                accepted = (
                    start == 0
                    if kind == "^"
                    else start == len(value)
                    if kind == "$"
                    else (word(start - 1) != word(start)) == (kind == "b")
                )
                return frozenset((start,)) if accepted else frozenset()
            if kind == "|":
                return frozenset(end for child in node.children for end in ends(child, start))
            current = frozenset((start,))
            if kind == "s":
                for child in node.children:
                    current = advance(child, current)
                    if not current:
                        break
                return current
            count, child = 0, node.children[0]
            while count < node.minimum and current:
                following = advance(child, current)
                count += 1
                if following == current:
                    count = node.minimum
                    break
                current = following
            result = set(current)
            while current and (node.maximum is None or count < node.maximum):
                following = advance(child, current)
                if following == current:
                    break
                result.update(following)
                current = following
                count += 1
            return frozenset(result)

        return any(ends(self.tree, start) for start in range(len(value) + 1))


@lru_cache(maxsize=256)
def compile_pattern(source):
    return Pattern(source)
