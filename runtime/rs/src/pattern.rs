//! Nightseam's regular subset of ECMAScript Unicode patterns.
//!
//! Parse the dialect directly so native regex syntax, Unicode character classes,
//! and expansion limits cannot change a declaration's meaning between runtimes.

use std::collections::HashMap;

pub(crate) fn check(source: &str) -> Result<(), ()> {
    Parser::new(source).parse().map(|_| ())
}

pub(crate) fn is_match(source: &str, text: &str) -> Result<bool, ()> {
    let pattern = Parser::new(source).parse()?;
    let mut state = MatchState {
        pattern: &pattern,
        input: text.chars().map(u32::from).collect(),
        memo: HashMap::new(),
    };
    Ok((0..=state.input.len()).any(|start| !state.ends(pattern.root, start).is_empty()))
}

type Characters = Vec<(u32, u32)>;

const DIGITS: &[(u32, u32)] = &[(0x30, 0x39)];
const WORDS: &[(u32, u32)] = &[(0x30, 0x39), (0x41, 0x5a), (0x5f, 0x5f), (0x61, 0x7a)];
// ECMAScript WhiteSpace and LineTerminator without case-folding flags.
const WHITESPACE: &[(u32, u32)] = &[
    (0x9, 0xd),
    (0x20, 0x20),
    (0xa0, 0xa0),
    (0x1680, 0x1680),
    (0x2000, 0x200a),
    (0x2028, 0x2029),
    (0x202f, 0x202f),
    (0x205f, 0x205f),
    (0x3000, 0x3000),
    (0xfeff, 0xfeff),
];

enum Kind {
    Character(Characters),
    Start,
    End,
    Boundary(bool),
    Sequence(Vec<usize>),
    Alternatives(Vec<usize>),
    Repeat {
        child: usize,
        min: u64,
        max: Option<u64>,
    },
}

struct Node {
    kind: Kind,
    width: u64,
}

struct Pattern {
    nodes: Vec<Node>,
    root: usize,
}

#[derive(Default)]
struct Group {
    branches: Vec<usize>,
    sequence: Vec<usize>,
}

struct Parser<'a> {
    source: &'a str,
    pos: usize,
    nodes: Vec<Node>,
}

impl<'a> Parser<'a> {
    fn new(source: &'a str) -> Self {
        Self {
            source,
            pos: 0,
            nodes: Vec::new(),
        }
    }

    fn peek(&self) -> Option<u8> {
        self.source.as_bytes().get(self.pos).copied()
    }

    fn character(&mut self) -> Result<char, ()> {
        let value = self.source[self.pos..].chars().next().ok_or(())?;
        self.pos += value.len_utf8();
        Ok(value)
    }

    fn push(&mut self, kind: Kind) -> usize {
        let width = match &kind {
            Kind::Character(_) => 1,
            Kind::Sequence(children) => children.iter().fold(0_u64, |width, child| {
                width.saturating_add(self.nodes[*child].width)
            }),
            Kind::Alternatives(children) => children
                .iter()
                .map(|child| self.nodes[*child].width)
                .min()
                .unwrap_or(0),
            Kind::Repeat { child, min, .. } => self.nodes[*child].width.saturating_mul(*min),
            _ => 0,
        };
        let index = self.nodes.len();
        self.nodes.push(Node { kind, width });
        index
    }

    fn sequence(&mut self, children: Vec<usize>) -> usize {
        if children.len() == 1 {
            children[0]
        } else {
            self.push(Kind::Sequence(children))
        }
    }

    fn group(&mut self, mut group: Group) -> usize {
        let last = self.sequence(group.sequence);
        if group.branches.is_empty() {
            return last;
        }
        group.branches.push(last);
        self.push(Kind::Alternatives(group.branches))
    }

    fn parse(mut self) -> Result<Pattern, ()> {
        // A stack avoids recursive parsing for deeply nested groups. Nodes use
        // arena indices too, so dropping a pattern cannot recurse through it.
        let mut groups = Vec::new();
        let mut current = Group::default();
        loop {
            let (atom, assertion) = match self.peek() {
                None => {
                    if !groups.is_empty() {
                        return Err(());
                    }
                    let root = self.group(current);
                    return Ok(Pattern {
                        nodes: self.nodes,
                        root,
                    });
                }
                Some(b'|') => {
                    self.pos += 1;
                    let sequence = self.sequence(std::mem::take(&mut current.sequence));
                    current.branches.push(sequence);
                    continue;
                }
                Some(b'(') => {
                    self.pos += 1;
                    if self.peek() == Some(b'?') {
                        if !self.source[self.pos..].starts_with("?:") {
                            return Err(());
                        }
                        self.pos += 2;
                    }
                    groups.push(std::mem::take(&mut current));
                    continue;
                }
                Some(b')') => {
                    self.pos += 1;
                    let outer = groups.pop().ok_or(())?;
                    let inner = self.group(std::mem::replace(&mut current, outer));
                    (inner, false)
                }
                _ => {
                    let kind = self.atom()?;
                    let assertion = matches!(kind, Kind::Start | Kind::End | Kind::Boundary(_));
                    (self.push(kind), assertion)
                }
            };
            let node = self.quantified(atom, assertion)?;
            current.sequence.push(node);
        }
    }

    fn quantified(&mut self, child: usize, assertion: bool) -> Result<usize, ()> {
        let (min, max) = match self.peek() {
            Some(b'*') => {
                self.pos += 1;
                (0, None)
            }
            Some(b'+') => {
                self.pos += 1;
                (1, None)
            }
            Some(b'?') => {
                self.pos += 1;
                (0, Some(1))
            }
            Some(b'{') => self.counts()?,
            _ => return Ok(child),
        };
        if assertion {
            return Err(());
        }
        // Greediness affects captures and match selection, neither of which
        // is exposed by the boolean validator.
        if self.peek() == Some(b'?') {
            self.pos += 1;
        }
        Ok(self.push(Kind::Repeat { child, min, max }))
    }

    fn digits(&mut self) -> Result<(usize, usize), ()> {
        let start = self.pos;
        while self.peek().is_some_and(|c| c.is_ascii_digit()) {
            self.pos += 1;
        }
        if self.pos == start {
            Err(())
        } else {
            Ok((start, self.pos))
        }
    }

    fn counts(&mut self) -> Result<(u64, Option<u64>), ()> {
        self.pos += 1;
        let lower = self.digits()?;
        let mut upper = Some(lower);
        if self.peek() == Some(b',') {
            self.pos += 1;
            upper = self.digits().ok();
        }
        if self.peek() != Some(b'}') {
            return Err(());
        }
        self.pos += 1;
        let lower = decimal(&self.source[lower.0..lower.1]);
        let upper = upper.map(|(start, end)| decimal(&self.source[start..end]));
        if upper.is_some_and(|upper| (upper.len(), upper) < (lower.len(), lower)) {
            return Err(());
        }
        // Check order before saturation: decimal counts have no machine-sized
        // bound, but any nonempty match is bounded by the actual input length.
        Ok((saturated_count(lower), upper.map(saturated_count)))
    }

    fn atom(&mut self) -> Result<Kind, ()> {
        if self.source[self.pos..].starts_with("[[:") {
            return Err(());
        }
        Ok(match self.character()? {
            '^' => Kind::Start,
            '$' => Kind::End,
            '.' => Kind::Character(complement(vec![(10, 10), (13, 13), (0x2028, 0x2029)])),
            '[' => Kind::Character(self.class()?),
            '\\' => self.escape(false)?,
            '*' | '+' | '?' | '{' | '}' | ']' => return Err(()),
            c => literal(u32::from(c)),
        })
    }

    fn class(&mut self) -> Result<Characters, ()> {
        let negated = self.peek() == Some(b'^');
        if negated {
            self.pos += 1;
        }
        let mut result = Vec::new();
        while self.peek().is_some_and(|c| c != b']') {
            let left = self.class_atom()?;
            if self.peek() == Some(b'-')
                && self
                    .source
                    .as_bytes()
                    .get(self.pos + 1)
                    .is_some_and(|c| *c != b']')
            {
                self.pos += 1;
                let right = self.class_atom()?;
                let lo = single(&left).ok_or(())?;
                let hi = single(&right).ok_or(())?;
                if lo > hi {
                    return Err(());
                }
                result.push((lo, hi));
            } else {
                result.extend(left);
            }
        }
        if self.peek() != Some(b']') {
            return Err(());
        }
        self.pos += 1;
        Ok(if negated {
            complement(result)
        } else {
            normalized(result)
        })
    }

    fn class_atom(&mut self) -> Result<Characters, ()> {
        let c = self.character()?;
        if c != '\\' {
            return Ok(vec![(u32::from(c), u32::from(c))]);
        }
        match self.escape(true)? {
            Kind::Character(set) => Ok(set),
            _ => Err(()),
        }
    }

    fn escape(&mut self, class: bool) -> Result<Kind, ()> {
        let c = self.character()?;
        Ok(match c {
            'd' => Kind::Character(DIGITS.to_vec()),
            'D' => Kind::Character(complement(DIGITS.to_vec())),
            'w' => Kind::Character(WORDS.to_vec()),
            'W' => Kind::Character(complement(WORDS.to_vec())),
            's' => Kind::Character(WHITESPACE.to_vec()),
            'S' => Kind::Character(complement(WHITESPACE.to_vec())),
            'b' if !class => Kind::Boundary(true),
            'B' if !class => Kind::Boundary(false),
            'b' => literal(8),
            'f' => literal(12),
            'n' => literal(10),
            'r' => literal(13),
            't' => literal(9),
            'v' => literal(11),
            '0' => {
                if self.peek().is_some_and(|c| c.is_ascii_digit()) {
                    return Err(());
                }
                literal(0)
            }
            'c' => {
                let c = self.character()?;
                if !c.is_ascii_alphabetic() {
                    return Err(());
                }
                literal(u32::from(c) & 31)
            }
            'x' => literal(self.hex(2)?),
            'u' => literal(self.unicode()?),
            c if "^$\\.*+?()[]{}|/".contains(c) || class && c == '-' => literal(u32::from(c)),
            _ => return Err(()),
        })
    }

    fn unicode(&mut self) -> Result<u32, ()> {
        if self.peek() == Some(b'{') {
            self.pos += 1;
            let start = self.pos;
            let mut value = 0_u32;
            while self.peek().is_some_and(|c| c.is_ascii_hexdigit()) {
                let digit = char::from(self.peek().ok_or(())?).to_digit(16).ok_or(())?;
                value = value
                    .checked_mul(16)
                    .and_then(|v| v.checked_add(digit))
                    .ok_or(())?;
                self.pos += 1;
            }
            if self.pos == start || self.peek() != Some(b'}') || value > 0x10ffff {
                return Err(());
            }
            self.pos += 1;
            return Ok(value);
        }
        let mut value = self.hex(4)?;
        if (0xd800..=0xdbff).contains(&value) && self.source[self.pos..].starts_with("\\u") {
            let end = self.pos;
            self.pos += 2;
            if let Ok(trail @ 0xdc00..=0xdfff) = self.hex(4) {
                value = 0x10000 + ((value - 0xd800) << 10) + trail - 0xdc00;
            } else {
                self.pos = end;
            }
        }
        // Surrogates may occur in the pattern, but never equal a Rust string's
        // scalar values. Keeping them as u32 avoids replacing them with U+FFFD.
        Ok(value)
    }

    fn hex(&mut self, count: usize) -> Result<u32, ()> {
        let mut value = 0;
        for _ in 0..count {
            let c = self.peek().filter(u8::is_ascii_hexdigit).ok_or(())?;
            value = value * 16 + char::from(c).to_digit(16).ok_or(())?;
            self.pos += 1;
        }
        Ok(value)
    }
}

fn decimal(value: &str) -> &str {
    let value = value.trim_start_matches('0');
    if value.is_empty() { "0" } else { value }
}

fn saturated_count(value: &str) -> u64 {
    value.bytes().fold(0_u64, |n, digit| {
        n.saturating_mul(10).saturating_add(u64::from(digit - b'0'))
    })
}

fn literal(value: u32) -> Kind {
    Kind::Character(vec![(value, value)])
}

fn single(set: &[(u32, u32)]) -> Option<u32> {
    match set {
        [(lo, hi)] if lo == hi => Some(*lo),
        _ => None,
    }
}

fn normalized(mut set: Characters) -> Characters {
    set.sort_unstable();
    let mut result: Characters = Vec::new();
    for (lo, hi) in set {
        if let Some(last) = result.last_mut().filter(|last| lo <= last.1 + 1) {
            last.1 = last.1.max(hi);
        } else {
            result.push((lo, hi));
        }
    }
    result
}

fn complement(set: Characters) -> Characters {
    let mut result = Vec::new();
    let mut next = 0;
    for (lo, hi) in normalized(set) {
        if next < lo {
            result.push((next, lo - 1));
        }
        next = hi + 1;
    }
    if next <= 0x10ffff {
        result.push((next, 0x10ffff));
    }
    result
}

struct MatchState<'a> {
    pattern: &'a Pattern,
    input: Vec<u32>,
    memo: HashMap<(usize, usize), Vec<usize>>,
}

impl MatchState<'_> {
    fn ends(&mut self, index: usize, start: usize) -> Vec<usize> {
        let node = &self.pattern.nodes[index];
        if node.width > (self.input.len() - start) as u64 {
            return Vec::new();
        }
        if let Some(result) = self.memo.get(&(index, start)) {
            return result.clone();
        }
        let result = match &node.kind {
            Kind::Character(set) => {
                if self
                    .input
                    .get(start)
                    .is_some_and(|c| set.iter().any(|(lo, hi)| lo <= c && c <= hi))
                {
                    vec![start + 1]
                } else {
                    Vec::new()
                }
            }
            Kind::Start => {
                if start == 0 {
                    vec![start]
                } else {
                    Vec::new()
                }
            }
            Kind::End => {
                if start == self.input.len() {
                    vec![start]
                } else {
                    Vec::new()
                }
            }
            Kind::Boundary(boundary) => {
                let before = start.checked_sub(1).is_some_and(|at| self.word(at));
                if (before != self.word(start)) == *boundary {
                    vec![start]
                } else {
                    Vec::new()
                }
            }
            Kind::Sequence(children) => {
                let mut result = vec![start];
                for child in children {
                    result = self.advance(*child, &result);
                    if result.is_empty() {
                        break;
                    }
                }
                result
            }
            Kind::Alternatives(children) => {
                let mut result = Vec::new();
                for child in children {
                    result.extend(self.ends(*child, start));
                }
                unique(result)
            }
            Kind::Repeat { child, min, max } => {
                let mut current = vec![start];
                let mut count = 0_u64;
                while count < *min && !current.is_empty() {
                    let next = self.advance(*child, &current);
                    count += 1;
                    if next == current {
                        count = *min;
                        break;
                    }
                    current = next;
                }
                let mut result = current.clone();
                if !current.is_empty() {
                    while max.is_none_or(|max| count < max) {
                        let next = self.advance(*child, &current);
                        if next.is_empty() || next == current {
                            break;
                        }
                        result.extend_from_slice(&next);
                        current = next;
                        count = count.saturating_add(1);
                    }
                }
                unique(result)
            }
        };
        self.memo.insert((index, start), result.clone());
        result
    }

    fn advance(&mut self, child: usize, positions: &[usize]) -> Vec<usize> {
        let mut result = Vec::new();
        for start in positions {
            result.extend(self.ends(child, *start));
        }
        unique(result)
    }

    fn word(&self, at: usize) -> bool {
        self.input
            .get(at)
            .is_some_and(|c| WORDS.iter().any(|(lo, hi)| lo <= c && c <= hi))
    }
}

// Transitions never move backward. An unchanged set of positions is a fixed
// point, including repetitions whose child can match the empty string.
fn unique(mut positions: Vec<usize>) -> Vec<usize> {
    positions.sort_unstable();
    positions.dedup();
    positions
}

#[cfg(test)]
mod tests {
    use super::{check, is_match};

    #[test]
    fn shared_pattern_syntax_and_values() {
        let table: serde_json::Value =
            serde_json::from_str(include_str!("../../../conformance/tables/validator.json"))
                .unwrap();
        for row in table["patterns"].as_array().unwrap() {
            let pattern = row["pattern"].as_str().unwrap();
            assert_eq!(
                check(pattern).is_ok(),
                row["valid"].as_bool().unwrap(),
                "syntax {pattern:?}"
            );
        }
        for row in table["patternValues"].as_array().unwrap() {
            let pattern = row["pattern"].as_str().unwrap();
            let value = row["value"].as_str().unwrap();
            assert_eq!(
                is_match(pattern, value),
                Ok(row["valid"].as_bool().unwrap()),
                "pattern {pattern:?} on {value:?}"
            );
        }
    }

    #[test]
    fn repetition_is_bounded_by_input_instead_of_declared_count() {
        for (pattern, value, expected) in [
            ("^a{1001}$", "a".repeat(1001), true),
            ("^a{1001}$", "a".repeat(1000), false),
            ("^(?:a{500}){3}$", "a".repeat(1500), true),
            ("^a{1001,1003}$", "a".repeat(1004), false),
            ("^a{1001,}$", "a".repeat(1004), true),
            ("^(?:a?){999999999999999999999999}$", "aaa".into(), true),
            ("^(?:a?){999999999999999999999999}$", "".into(), true),
            ("^(?:a?){999999999999999999999999}$", "b".into(), false),
            ("^(?:a|){999999999999999999999999}$", "aaa".into(), true),
            ("^(?:^){999999999999999999999999}$", "".into(), true),
            ("^a{0002,0003}$", "aa".into(), true),
            ("^a{0002,0003}$", "aaaa".into(), false),
        ] {
            assert_eq!(is_match(pattern, &value), Ok(expected), "{pattern:?}");
        }
    }

    #[test]
    fn empty_alternatives_assertions_and_lazy_quantifiers() {
        for (pattern, value, expected) in [
            ("a|", "", true),
            ("|a", "", true),
            ("(?:)", "", true),
            ("^(a|b)+?c*?d??$", "abcc", true),
            ("^(a|b)+?c*?d??$", "abce", false),
            (r"\bword\b", "a word!", true),
            (r"\bword\b", "sword!", false),
            (r"\B", "", true),
            ("a$", "a\n", false),
            (r"^[\uD83D\uDE00]$", "😀", true),
            (r"^\uD800a$", "�a", false),
            (r"^[--0]$", "/", true),
        ] {
            assert_eq!(is_match(pattern, value), Ok(expected), "{pattern:?}");
        }
        for pattern in [
            "(", ")", "a{2,1}", "a{1,2,3}", "^*", r"\b+", "a++", r"[a-\s]", "[z-a]", r"\x0",
            r"\u00", r"\u{}", r"\c_", r"[\B]", "a**", "a???",
        ] {
            assert_eq!(check(pattern), Err(()), "{pattern:?}");
        }
        let nested = format!("{}a{}", "(?:".repeat(1001), ")".repeat(1001));
        assert_eq!(is_match(&nested, "a"), Ok(true));
    }
}
