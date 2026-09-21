use crate::{pattern, wire::check_unicode};
use serde_json::{Map, Value};
use std::{
    collections::{BTreeMap, BTreeSet},
    fmt,
    sync::Arc,
};

/// A refusal retains the same location and diagnostic in every runtime.
/// Declaration revision mismatches additionally carry `contract_mismatch`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ValidationError {
    pub message: String,
    pub code: Option<String>,
}

impl fmt::Display for ValidationError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}
impl std::error::Error for ValidationError {}
impl From<String> for ValidationError {
    fn from(message: String) -> Self {
        Self {
            message,
            code: None,
        }
    }
}
type Result<T> = std::result::Result<T, ValidationError>;
fn error(message: impl Into<String>) -> ValidationError {
    message.into().into()
}
fn expected(at: &str, want: &str) -> ValidationError {
    error(format!("{at}: expected {want}"))
}

/// A family's descriptor, imported families, and lexical generic bindings.
/// Cloning and binding preserve the original schema and arguments' scopes.
#[derive(Clone, Debug)]
pub struct Schema {
    descriptor: Arc<Descriptor>,
    scope: Arc<BTreeMap<String, Argument>>,
}

#[derive(Debug)]
struct Descriptor {
    types: BTreeMap<String, Arc<Value>>,
    parameters: Vec<Parameter>,
    imported: BTreeMap<String, Schema>,
    digest: String,
    drawn: BTreeMap<String, Expression>,
}
#[derive(Clone, Debug)]
struct Parameter {
    name: String,
    of: String,
}
#[derive(Clone, Debug)]
enum Argument {
    Type(Expression),
    Family(Schema),
    UnboundFamily,
}
#[derive(Clone, Debug)]
struct Expression {
    schema: Schema,
    value: Value,
    scope: Arc<BTreeMap<String, Argument>>,
    aliases: BTreeSet<usize>,
}
struct Resolved {
    expression: Expression,
    definition: Option<Arc<Value>>,
    name: String,
}
struct ScopedField {
    field: Value,
    expression: Expression,
}

fn parameters(value: &Value) -> Vec<Parameter> {
    array(&value["parameters"])
        .iter()
        .map(|p| Parameter {
            name: text(&p["name"]).into(),
            of: text(&p["of"]).into(),
        })
        .collect()
}
fn text(value: &Value) -> &str {
    value.as_str().unwrap_or("")
}
fn array(value: &Value) -> &[Value] {
    value.as_array().map(Vec::as_slice).unwrap_or(&[])
}
fn sorted_keys<T>(map: &BTreeMap<String, T>) -> Vec<&String> {
    let mut keys: Vec<_> = map.keys().collect();
    keys.sort_by(|a, b| a.encode_utf16().cmp(b.encode_utf16()));
    keys
}
fn object_keys(map: &Map<String, Value>) -> Vec<&String> {
    let mut keys: Vec<_> = map.keys().collect();
    keys.sort_by(|a, b| a.encode_utf16().cmp(b.encode_utf16()));
    keys
}
fn quote(text: &str) -> String {
    // Go's JSON encoder escapes HTML and the two JavaScript line separators.
    serde_json::to_string(text)
        .expect("strings serialize")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('&', "\\u0026")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
}
fn valid_digest(digest: &str) -> bool {
    digest.is_empty()
        || (digest.len() == 64
            && digest
                .bytes()
                .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c)))
}
fn check_patterns(value: &Value) -> Result<()> {
    match value {
        Value::Object(obj) => {
            if let Some(pattern) = obj.get("pattern").and_then(Value::as_str) {
                pattern::check(pattern).map_err(|_| {
                    error(format!(
                        "pattern {}: outside Nightseam dialect",
                        quote(pattern)
                    ))
                })?;
            }
            for key in object_keys(obj) {
                check_patterns(&obj[key])?;
            }
        }
        Value::Array(items) => {
            for item in items {
                check_patterns(item)?;
            }
        }
        _ => (),
    }
    Ok(())
}

impl Schema {
    /// Parse a family descriptor. An empty digest leaves its identity unspecified.
    pub fn new(wire: &[u8], digest: &str, imported: BTreeMap<String, Schema>) -> Result<Self> {
        if !valid_digest(digest) {
            return Err(error(
                "schema.digest: expected empty or lowercase SHA-256 digest",
            ));
        }
        check_unicode(wire)?;
        let wire: Value = serde_json::from_slice(wire).map_err(|e| error(e.to_string()))?;
        let types = wire["types"]
            .as_object()
            .ok_or_else(|| error("expected family descriptor with types"))?;
        for name in object_keys(types) {
            check_patterns(&types[name])?;
        }
        Ok(Self {
            descriptor: Arc::new(Descriptor {
                types: types
                    .iter()
                    .map(|(k, v)| (k.clone(), Arc::new(v.clone())))
                    .collect(),
                parameters: parameters(&wire),
                imported,
                digest: digest.into(),
                drawn: BTreeMap::new(),
            }),
            scope: Arc::new(BTreeMap::new()),
        })
    }

    /// Bind type expressions in this schema's current scope and explicit families.
    /// Qualified type slots such as `S.Envelope` also forward as family arguments.
    pub fn bind(&self, types: BTreeMap<String, Value>, families: BTreeMap<String, Schema>) -> Self {
        let mut scope = (*self.scope).clone();
        let mut drawn_families = BTreeSet::new();
        for (name, value) in types {
            if let Some((family, member)) = name.split_once('.') {
                if !family.is_empty() && !member.is_empty() {
                    drawn_families.insert(family.to_owned());
                }
            }
            scope.insert(name, Argument::Type(self.expression(value)));
        }
        for family in drawn_families {
            let drawn: BTreeMap<String, Expression> = scope
                .iter()
                .filter_map(|(name, arg)| {
                    let (prefix, member) = name.split_once('.')?;
                    match arg {
                        Argument::Type(expression) if prefix == family => {
                            Some((member.into(), expression.clone()))
                        }
                        _ => None,
                    }
                })
                .collect();
            let types = drawn
                .keys()
                .map(|name| (name.clone(), Arc::new(serde_json::json!({"kind":"alias"}))))
                .collect();
            scope.insert(
                family,
                Argument::Family(Self {
                    descriptor: Arc::new(Descriptor {
                        types,
                        parameters: Vec::new(),
                        imported: BTreeMap::new(),
                        digest: String::new(),
                        drawn,
                    }),
                    scope: Arc::new(BTreeMap::new()),
                }),
            );
        }
        for (name, family) in families {
            scope.insert(name, Argument::Family(family));
        }
        Self {
            descriptor: self.descriptor.clone(),
            scope: Arc::new(scope),
        }
    }

    fn expression(&self, value: Value) -> Expression {
        Expression {
            schema: self.clone(),
            value,
            scope: self.scope.clone(),
            aliases: BTreeSet::new(),
        }
    }

    /// Validate a named type at the root of a raw JSON value.
    pub fn validate_raw(&self, name: &str, data: &[u8]) -> Result<()> {
        self.validate_raw_at(name, data, "$")
    }
    /// Validate a named type, rooting diagnostics at a supplied location.
    pub fn validate_raw_at(&self, name: &str, data: &[u8], at: &str) -> Result<()> {
        self.validate_expression_raw_at(&Value::String(name.into()), data, at)
    }
    /// Validate one JSON value against a type expression, retaining numeric tokens.
    pub fn validate_expression_raw(&self, expression: &Value, data: &[u8]) -> Result<()> {
        self.validate_expression_raw_at(expression, data, "$")
    }
    /// Validate an expression and root its diagnostic at a supplied location.
    pub fn validate_expression_raw_at(
        &self,
        expression: &Value,
        data: &[u8],
        at: &str,
    ) -> Result<()> {
        check_unicode(data)?;
        check_patterns(expression)?;
        for name in sorted_keys(&self.scope) {
            if let Argument::Type(argument) = &self.scope[name] {
                check_patterns(&argument.value)?;
            }
        }
        let mut values = serde_json::Deserializer::from_slice(data).into_iter::<Value>();
        let value = values
            .next()
            .ok_or_else(|| error("expected exactly one JSON value"))?
            .map_err(|e| error(e.to_string()))?;
        if values.next().is_some() {
            return Err(error("expected exactly one JSON value"));
        }
        self.expression(expression.clone()).validate(&value, at)
    }
    /// Validate an already decoded JSON value. Rust strings are Unicode scalar strings.
    pub fn validate_value(&self, expression: &Value, value: &Value) -> Result<()> {
        self.validate_expression_raw(
            expression,
            &serde_json::to_vec(value).map_err(|e| error(e.to_string()))?,
        )
    }
    /// Return a record's wire field names, inherited fields first.
    pub fn fields(&self, name: &str) -> Vec<String> {
        self.expression(name.into())
            .resolve("$", false)
            .and_then(|r| r.fields("$", &mut BTreeSet::new()))
            .map(|fields| {
                fields
                    .iter()
                    .map(|f| text(&f.field["name"]).into())
                    .collect()
            })
            .unwrap_or_default()
    }

    fn single_family_parameter(&self) -> Option<&Parameter> {
        let mut families = self
            .descriptor
            .parameters
            .iter()
            .filter(|p| !p.of.is_empty());
        let first = families.next()?;
        families.next().is_none().then_some(first)
    }

    fn free_parameters(
        &self,
        definition: &Arc<Value>,
        seen: &mut BTreeSet<usize>,
    ) -> Vec<Parameter> {
        let identity = Arc::as_ptr(definition) as usize;
        if !seen.insert(identity) {
            return Vec::new();
        }
        let mut used = BTreeSet::new();
        self.walk_free(&definition["type"], seen, &mut used);
        for field in array(&definition["fields"]) {
            self.walk_free(&field["type"], seen, &mut used);
        }
        for base in array(&definition["extends"]) {
            self.walk_base_free(base, seen, &mut used);
        }
        if let Some(variants) = definition["variants"].as_object() {
            for variant in variants.values() {
                self.walk_free(variant, seen, &mut used);
            }
        }
        seen.remove(&identity);
        self.descriptor
            .parameters
            .iter()
            .filter(|p| used.contains(&p.name))
            .cloned()
            .collect()
    }

    fn walk_base_free(
        &self,
        base: &Value,
        seen: &mut BTreeSet<usize>,
        used: &mut BTreeSet<String>,
    ) {
        if let Some(fillers) = base.get("with").and_then(Value::as_object) {
            for filler in fillers.values() {
                self.walk_free(filler, seen, used);
            }
        } else {
            self.walk_free(base, seen, used);
        }
    }

    fn walk_free(&self, value: &Value, seen: &mut BTreeSet<usize>, used: &mut BTreeSet<String>) {
        match value {
            Value::String(name) => {
                let prefix = name.split('.').next().unwrap_or("");
                if self.descriptor.parameters.iter().any(|p| p.name == prefix) {
                    used.insert(prefix.into());
                    return;
                }
                if let Some((family, member)) = name.split_once('.') {
                    if let Some(imported) = self.descriptor.imported.get(family) {
                        if let Some(target) = imported.descriptor.types.get(member) {
                            let mut needed = imported.free_parameters(target, seen);
                            needed.extend(parameters(target));
                            if needed.iter().any(|p| !p.of.is_empty()) {
                                if let Some(parameter) = self.single_family_parameter() {
                                    used.insert(parameter.name.clone());
                                }
                            }
                        }
                    }
                } else if let Some(target) = self.descriptor.types.get(name) {
                    used.extend(
                        self.free_parameters(target, seen)
                            .into_iter()
                            .map(|p| p.name),
                    );
                }
            }
            Value::Object(obj) => {
                for key in ["array", "map", "nullable"] {
                    if let Some(inner) = obj.get(key) {
                        self.walk_free(inner, seen, used);
                        return;
                    }
                }
                if let Some(target) = obj.get("apply").and_then(Value::as_str) {
                    if !target.contains('.') {
                        if let Some(definition) = self.descriptor.types.get(target) {
                            used.extend(
                                self.free_parameters(definition, seen)
                                    .into_iter()
                                    .map(|p| p.name),
                            );
                        }
                    }
                    if let Some(fillers) = obj.get("with").and_then(Value::as_object) {
                        for filler in fillers.values() {
                            self.walk_free(filler, seen, used);
                        }
                    }
                    return;
                }
                if let Some(target) = obj.get("ref").and_then(Value::as_str) {
                    if let Some(entity) = self.descriptor.types.get(target) {
                        for field in array(&entity["fields"]) {
                            if field["name"] == entity["key"] {
                                self.walk_free(&field["type"], seen, used);
                            }
                        }
                    }
                    return;
                }
                if obj.contains_key("kind") {
                    for field in array(&value["fields"]) {
                        self.walk_free(&field["type"], seen, used);
                    }
                    for base in array(&value["extends"]) {
                        self.walk_base_free(base, seen, used);
                    }
                    if let Some(variants) = value["variants"].as_object() {
                        for variant in variants.values() {
                            self.walk_free(variant, seen, used);
                        }
                    }
                }
            }
            _ => (),
        }
    }
}

impl Expression {
    fn child(&self, value: Value) -> Self {
        Self {
            value,
            aliases: BTreeSet::new(),
            ..self.clone()
        }
    }
    fn unbound_family(&self, name: &str) -> bool {
        match self.scope.get(name) {
            Some(Argument::UnboundFamily) => true,
            Some(_) => false,
            None => self
                .schema
                .descriptor
                .parameters
                .iter()
                .any(|p| p.name == name && !p.of.is_empty()),
        }
    }
    fn named(&self, name: &str, at: &str) -> Result<(Self, Arc<Value>, String)> {
        let mut expression = self.clone();
        let mut name = name;
        if let Some((family, member)) = name.split_once('.') {
            let first = family
                .as_bytes()
                .first()
                .ok_or_else(|| expected(at, "known family"))?;
            let schema = if first.is_ascii_uppercase() {
                match self.scope.get(family) {
                    Some(Argument::Family(schema)) => schema,
                    _ => {
                        return Err(expected(
                            at,
                            &format!("a binding of the parameter {family}"),
                        ));
                    }
                }
            } else {
                self.schema
                    .descriptor
                    .imported
                    .get(family)
                    .ok_or_else(|| expected(at, "known family"))?
            };
            expression = schema.expression(member.into());
            if first.is_ascii_lowercase() {
                if let Some(source) = self.schema.single_family_parameter() {
                    if let Some(target) = schema.descriptor.types.get(member) {
                        let mut scope = (*schema.scope).clone();
                        let mut needed = schema.free_parameters(target, &mut BTreeSet::new());
                        needed.extend(parameters(target));
                        for parameter in needed.into_iter().filter(|p| !p.of.is_empty()) {
                            if let Some(argument) = self.scope.get(&source.name) {
                                scope.insert(parameter.name, argument.clone());
                            } else if self.unbound_family(&source.name) {
                                scope.insert(parameter.name, Argument::UnboundFamily);
                            }
                        }
                        expression.scope = Arc::new(scope);
                    }
                }
            }
            name = member;
        }
        let definition = expression
            .schema
            .descriptor
            .types
            .get(name)
            .ok_or_else(|| expected(at, "known type"))?
            .clone();
        expression.value = name.into();
        Ok((expression, definition, name.into()))
    }

    fn resolve(mut self, at: &str, mut inheritance: bool) -> Result<Resolved> {
        let mut aliases = self.aliases.clone();
        loop {
            let (definition, name) = match self.value.clone() {
                Value::String(name) => {
                    if let Some(argument) = self.scope.get(&name) {
                        let Argument::Type(argument) = argument else {
                            return Err(expected(at, "type argument"));
                        };
                        self = argument.clone();
                        aliases = self.aliases.clone();
                        continue;
                    }
                    if let Some((family, _)) = name.split_once('.') {
                        if self.unbound_family(family) {
                            return Ok(Resolved {
                                expression: self.child("json".into()),
                                definition: None,
                                name: String::new(),
                            });
                        }
                    }
                    if [
                        "json",
                        "string",
                        "boolean",
                        "number",
                        "integer",
                        "timestamp",
                    ]
                    .contains(&name.as_str())
                    {
                        return Ok(Resolved {
                            expression: self,
                            definition: None,
                            name: String::new(),
                        });
                    }
                    let (target, definition, name) = self.named(&name, at)?;
                    self = target;
                    (definition, name)
                }
                Value::Object(obj) => {
                    if let Some(reference) = obj.get("apply").and_then(Value::as_str) {
                        let (mut target, definition, name) = self.named(reference, at)?;
                        let fillers = obj
                            .get("with")
                            .and_then(Value::as_object)
                            .ok_or_else(|| expected(at, "application arguments"))?;
                        let mut scope = (*target.scope).clone();
                        let mut needed = if inheritance || reference.contains('.') {
                            target
                                .schema
                                .free_parameters(&definition, &mut BTreeSet::new())
                        } else {
                            Vec::new()
                        };
                        needed.extend(parameters(&definition));
                        let mut allowed = BTreeSet::new();
                        for parameter in needed {
                            allowed.insert(parameter.name.clone());
                            let filler = fillers.get(&parameter.name).ok_or_else(|| {
                                expected(at, &format!("an argument for {}", parameter.name))
                            })?;
                            let argument = if parameter.of.is_empty() {
                                let mut captured = self.child(filler.clone());
                                captured.aliases = aliases.clone();
                                Argument::Type(captured)
                            } else {
                                let family = filler.as_str().ok_or_else(|| {
                                    expected(at, &format!("family argument for {}", parameter.name))
                                })?;
                                if self.unbound_family(family) {
                                    Argument::UnboundFamily
                                } else {
                                    let schema = match self.scope.get(family) {
                                        Some(Argument::Family(schema)) => Some(schema),
                                        Some(_) => None,
                                        None => self.schema.descriptor.imported.get(family),
                                    };
                                    Argument::Family(
                                        schema
                                            .ok_or_else(|| {
                                                expected(
                                                    at,
                                                    &format!(
                                                        "known family argument for {}",
                                                        parameter.name
                                                    ),
                                                )
                                            })?
                                            .clone(),
                                    )
                                }
                            };
                            scope.insert(parameter.name, argument);
                        }
                        for parameter in object_keys(fillers) {
                            if !allowed.contains(parameter) {
                                return Err(expected(at, &format!("known parameter {parameter}")));
                            }
                        }
                        target.scope = Arc::new(scope);
                        self = target;
                        (definition, name)
                    } else if obj.contains_key("kind") {
                        let name = text(&obj["kind"]).to_owned();
                        (Arc::new(Value::Object(obj)), name)
                    } else {
                        return Ok(Resolved {
                            expression: self,
                            definition: None,
                            name: String::new(),
                        });
                    }
                }
                _ => return Err(expected(at, "type expression")),
            };
            inheritance = false;
            if text(&definition["kind"]) == "alias" {
                if !aliases.insert(Arc::as_ptr(&definition) as usize) {
                    return Err(expected(at, "acyclic type expression"));
                }
                if let Some(drawn) = self.schema.descriptor.drawn.get(&name) {
                    self = drawn.clone();
                    aliases = self.aliases.clone();
                } else {
                    self.value = definition["type"].clone();
                }
                continue;
            }
            return Ok(Resolved {
                expression: self,
                definition: Some(definition),
                name,
            });
        }
    }

    fn inherited(&self, base: &Value, at: &str) -> Result<Resolved> {
        if let Some(name) = base.as_str() {
            let (owner, definition, _) = self.named(name, at)?;
            if !parameters(&definition).is_empty()
                || !owner
                    .schema
                    .free_parameters(&definition, &mut BTreeSet::new())
                    .is_empty()
            {
                return Err(expected(
                    at,
                    &format!("explicit application of generic base {name}"),
                ));
            }
        }
        self.child(base.clone()).resolve(at, true)
    }
    fn nullable(&self, at: &str) -> Result<bool> {
        Ok(self
            .clone()
            .resolve(at, false)?
            .expression
            .value
            .get("nullable")
            .is_some())
    }

    fn validate(self, value: &Value, at: &str) -> Result<()> {
        let resolved = self.resolve(at, false)?;
        if let Some(definition) = &resolved.definition {
            return resolved.validate_definition(definition, value, at);
        }
        let expression = &resolved.expression;
        if let Value::Object(obj) = &expression.value {
            if let Some(inner) = obj.get("nullable") {
                return if value.is_null() {
                    Ok(())
                } else {
                    expression.child(inner.clone()).validate(value, at)
                };
            }
            if let Some(literal) = obj.get("literal") {
                let literal = literal
                    .as_str()
                    .filter(|s| !s.is_empty())
                    .ok_or_else(|| expected(at, "nonempty string literal"))?;
                return if value.as_str() == Some(literal) {
                    Ok(())
                } else {
                    Err(expected(at, &format!("literal {}", quote(literal))))
                };
            }
            if let Some(element) = obj.get("array") {
                let items = value.as_array().ok_or_else(|| expected(at, "array"))?;
                for (index, item) in items.iter().enumerate() {
                    expression
                        .child(element.clone())
                        .validate(item, &format!("{at}[{index}]"))?;
                }
                return Ok(());
            }
            if let Some(element) = obj.get("map") {
                let items = value.as_object().ok_or_else(|| expected(at, "object"))?;
                for key in object_keys(items) {
                    expression
                        .child(element.clone())
                        .validate(&items[key], &format!("{at}.{key}"))?;
                }
                return Ok(());
            }
            if let Some(entity) = obj.get("ref").and_then(Value::as_str) {
                let (target, definition, _) = expression
                    .named(entity, at)
                    .map_err(|_| expected(at, "known entity"))?;
                let fields = target
                    .resolve(at, false)?
                    .fields(at, &mut BTreeSet::new())?;
                for field in fields {
                    if field.field["name"] == definition["key"] {
                        return field.expression.validate(value, at);
                    }
                }
                return Err(expected(at, "entity with a key"));
            }
            if obj.contains_key("empty") {
                return if value.as_object().is_some_and(|o| o.is_empty()) {
                    Ok(())
                } else {
                    Err(expected(at, "empty object"))
                };
            }
            return Err(expected(at, "supported type expression"));
        }
        let name = text(&expression.value);
        match name {
            "json" => validate_json(value, at),
            "string" if value.is_string() => Ok(()),
            "boolean" if value.is_boolean() => Ok(()),
            "number" | "integer" => {
                let number = value.as_number().ok_or_else(|| expected(at, name))?;
                if !number.as_f64().is_some_and(f64::is_finite) {
                    return Err(expected(at, "finite number"));
                }
                if name == "integer" && !safe_integer(number.as_str()) {
                    return Err(expected(at, "JavaScript-safe integer"));
                }
                Ok(())
            }
            "timestamp" => {
                let value = value.as_str().ok_or_else(|| expected(at, "timestamp"))?;
                if valid_timestamp(value) {
                    Ok(())
                } else {
                    Err(expected(at, "RFC3339 timestamp"))
                }
            }
            _ => Err(expected(at, name)),
        }
    }
}

impl Resolved {
    fn fields(&self, at: &str, seen: &mut BTreeSet<usize>) -> Result<Vec<ScopedField>> {
        let definition = self
            .definition
            .as_ref()
            .ok_or_else(|| expected(at, "record"))?;
        let id = Arc::as_ptr(definition) as usize;
        if !seen.insert(id) {
            return Err(expected(at, "acyclic inheritance"));
        }
        let mut fields = Vec::new();
        for base in array(&definition["extends"]) {
            fields.extend(self.expression.inherited(base, at)?.fields(at, seen)?);
        }
        for field in array(&definition["fields"]) {
            fields.push(ScopedField {
                field: field.clone(),
                expression: self.expression.child(field["type"].clone()),
            });
        }
        seen.remove(&id);
        Ok(fields)
    }
    fn variants(
        &self,
        at: &str,
        seen: &mut BTreeSet<usize>,
    ) -> Result<BTreeMap<String, Expression>> {
        let definition = self
            .definition
            .as_ref()
            .filter(|d| text(&d["kind"]) == "union")
            .ok_or_else(|| expected(at, "union"))?;
        let id = Arc::as_ptr(definition) as usize;
        if !seen.insert(id) {
            return Err(expected(at, "acyclic inheritance"));
        }
        let mut variants = BTreeMap::new();
        for base in array(&definition["extends"]) {
            variants.extend(self.expression.inherited(base, at)?.variants(at, seen)?);
        }
        if let Some(own) = definition["variants"].as_object() {
            for (name, value) in own {
                variants.insert(name.clone(), self.expression.child(value.clone()));
            }
        }
        seen.remove(&id);
        Ok(variants)
    }
    fn validate_definition(&self, definition: &Value, value: &Value, at: &str) -> Result<()> {
        match text(&definition["kind"]) {
            "callable" => self.validate_callable(definition, value, at),
            "enum" => {
                if array(&definition["values"]).contains(value) {
                    Ok(())
                } else {
                    Err(expected(at, &self.name))
                }
            }
            "record" | "entity" => {
                let obj = value
                    .as_object()
                    .ok_or_else(|| expected(at, &format!("{} object", self.name)))?;
                let mut allowed = BTreeSet::new();
                for scoped in self.fields(at, &mut BTreeSet::new())? {
                    let field = &scoped.field;
                    let name = text(&field["name"]);
                    allowed.insert(name.to_owned());
                    let location = format!("{at}.{name}");
                    let Some(member) = obj.get(name) else {
                        if field["required"] == true {
                            return Err(error(format!("{location}: required field missing")));
                        }
                        continue;
                    };
                    if member.is_null() {
                        if field["nullable"] == true {
                            continue;
                        }
                        if !scoped.expression.nullable(&location)? {
                            return Err(error(format!("{location}: null is not permitted")));
                        }
                    }
                    scoped.expression.validate(member, &location)?;
                    if !member.is_null() {
                        constrain(field, member, &location)?;
                    }
                }
                for key in object_keys(obj) {
                    if allowed.contains(key) {
                        continue;
                    }
                    if definition["open"] != true {
                        return Err(error(format!("{at}.{key}: unknown field")));
                    }
                    validate_json(&obj[key], &format!("{at}.{key}"))?;
                }
                Ok(())
            }
            "union" => {
                let obj = value
                    .as_object()
                    .ok_or_else(|| expected(at, &format!("{} object", self.name)))?;
                let tag = text(&definition["tag"]);
                let tag_value = obj
                    .get(tag)
                    .ok_or_else(|| error(format!("{at}.{tag}: required field missing")))?;
                let tag_name = tag_value
                    .as_str()
                    .ok_or_else(|| expected(&format!("{at}.{tag}"), "known variant"))?;
                let mut variants = self.variants(at, &mut BTreeSet::new())?;
                let variant = variants
                    .remove(tag_name)
                    .ok_or_else(|| expected(&format!("{at}.{tag}"), "known variant"))?;
                let member = definition["value"]
                    .as_str()
                    .filter(|s| !s.is_empty())
                    .unwrap_or("value");
                let empty = variant
                    .value
                    .as_object()
                    .is_some_and(|o| o.len() == 1 && o.get("empty") == Some(&Value::Bool(true)));
                if !empty {
                    let wrapped = obj
                        .get(member)
                        .ok_or_else(|| error(format!("{at}.{member}: required field missing")))?;
                    variant.validate(wrapped, &format!("{at}.{member}"))?;
                }
                for key in object_keys(obj) {
                    if key != tag && (empty || key != member) {
                        return Err(error(format!("{at}.{key}: unknown field")));
                    }
                }
                Ok(())
            }
            _ => Err(expected(at, "supported type")),
        }
    }
    fn validate_callable(&self, definition: &Value, value: &Value, at: &str) -> Result<()> {
        let contract = text(&definition["contract"]);
        let obj = value
            .as_object()
            .ok_or_else(|| expected(at, &format!("a live reference to {contract}")))?;
        if obj
            .get("binding")
            .and_then(Value::as_str)
            .is_none_or(|v| v.is_empty())
        {
            return Err(error(format!(
                "{at}.binding: a live reference names the binding it refers to"
            )));
        }
        let actual = obj.get("contract").and_then(Value::as_str).ok_or_else(|| {
            error(format!(
                "{at}.contract: a live reference carries the declaration it implements"
            ))
        })?;
        if actual != contract {
            return Err(error(format!(
                "{at}.contract: the reference carries {actual} where {contract} is expected"
            )));
        }
        if let Some(digest) = obj.get("digest") {
            let digest = digest
                .as_str()
                .filter(|d| !d.is_empty() && valid_digest(d))
                .ok_or_else(|| expected(&format!("{at}.digest"), "lowercase SHA-256 digest"))?;
            let wanted = &self.expression.schema.descriptor.digest;
            if !wanted.is_empty() && digest != wanted {
                return Err(ValidationError {
                    code: Some("contract_mismatch".into()),
                    message: format!(
                        "{at}.digest: the reference to {contract} carries declaration digest {digest} where {wanted} is expected"
                    ),
                });
            }
        }
        for key in object_keys(obj) {
            if !["binding", "contract", "digest"].contains(&key.as_str()) {
                return Err(error(format!("{at}.{key}: unknown field")));
            }
        }
        Ok(())
    }
}

fn constrain(field: &Value, value: &Value, at: &str) -> Result<()> {
    for (key, comparison, label) in [
        ("min", std::cmp::Ordering::Less, "at least"),
        ("max", std::cmp::Ordering::Greater, "at most"),
    ] {
        if let Some(bound) = field.get(key) {
            if let (Some(value), Some(bound_value)) = (value.as_f64(), bound.as_f64()) {
                if value.partial_cmp(&bound_value) == Some(comparison) {
                    return Err(expected(at, &format!("{label} {bound}")));
                }
            } else if let Some(value) = value.as_str() {
                let raw = bound
                    .as_str()
                    .map(str::to_owned)
                    .unwrap_or_else(|| bound.to_string());
                if value.cmp(&raw) == comparison {
                    return Err(expected(
                        at,
                        &format!(
                            "at or {} {raw}",
                            if key == "min" { "after" } else { "before" }
                        ),
                    ));
                }
            }
        }
    }
    let length = value
        .as_str()
        .map(|s| s.chars().count())
        .or_else(|| value.as_array().map(Vec::len));
    if let Some(length) = length {
        for (key, comparison, label) in [
            ("min", std::cmp::Ordering::Less, "at least"),
            ("max", std::cmp::Ordering::Greater, "at most"),
        ] {
            if let Some(bound) = field["length"][key].as_u64() {
                if (length as u64).cmp(&bound) == comparison {
                    return Err(expected(at, &format!("a length of {label} {bound}")));
                }
            }
        }
    }
    if let (Some(pattern), Some(value)) = (
        field["pattern"].as_str().filter(|s| !s.is_empty()),
        value.as_str(),
    ) {
        if pattern::is_match(pattern, value) != Ok(true) {
            return Err(expected(at, &format!("a match of {pattern}")));
        }
    }
    Ok(())
}
fn validate_json(value: &Value, at: &str) -> Result<()> {
    match value {
        Value::Number(number) if !number.as_f64().is_some_and(f64::is_finite) => {
            return Err(expected(at, "finite JSON number"));
        }
        Value::Array(items) => {
            for (index, item) in items.iter().enumerate() {
                validate_json(item, &format!("{at}[{index}]"))?;
            }
        }
        Value::Object(items) => {
            for key in object_keys(items) {
                validate_json(&items[key], &format!("{at}.{key}"))?;
            }
        }
        _ => (),
    }
    Ok(())
}
fn safe_integer(raw: &str) -> bool {
    let raw = raw.strip_prefix('-').unwrap_or(raw);
    let (mantissa, exponent) = raw.split_once(['e', 'E']).unwrap_or((raw, "0"));
    let fraction = mantissa.split_once('.').map_or(0, |(_, v)| v.len());
    let mantissa = mantissa.replace('.', "");
    let mut digits = mantissa.trim_start_matches('0').to_owned();
    if digits.is_empty() {
        return true;
    }
    let Ok(power) = exponent.parse::<i64>() else {
        return false;
    };
    let limit = i64::try_from(raw.len())
        .unwrap_or(i64::MAX)
        .saturating_add(16);
    if power > limit || power < -limit {
        return false;
    }
    let scale = power - fraction as i64;
    if scale < 0 {
        let cut = (-scale) as usize;
        if cut > digits.len() {
            return false;
        }
        let split = digits.len() - cut;
        if !digits[split..].bytes().all(|c| c == b'0') {
            return false;
        }
        digits.truncate(split);
    } else {
        if digits.len() as i64 + scale > 16 {
            return false;
        }
        digits.push_str(&"0".repeat(scale as usize));
    }
    digits.len() <= 16
        && digits
            .parse::<u64>()
            .is_ok_and(|v| v <= 9_007_199_254_740_991)
}

fn valid_timestamp(value: &str) -> bool {
    // The reference is Go's RFC3339Nano parser: a four-digit year, real
    // calendar date, optional fractional seconds, and a numeric zone or Z.
    let bytes = value.as_bytes();
    if bytes.len() < 19 || bytes[4] != b'-' || bytes[7] != b'-' || bytes[10] != b'T' {
        return false;
    }
    fn number(bytes: &[u8]) -> Option<u32> {
        if bytes.is_empty() || !bytes.iter().all(u8::is_ascii_digit) {
            return None;
        }
        std::str::from_utf8(bytes).ok()?.parse().ok()
    }
    let (Some(year), Some(month), Some(day)) = (
        number(&bytes[..4]),
        number(&bytes[5..7]),
        number(&bytes[8..10]),
    ) else {
        return false;
    };
    let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
    let days = match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            if leap {
                29
            } else {
                28
            }
        }
        _ => return false,
    };
    if day == 0 || day > days {
        return false;
    }
    let time = &value[11..];
    let Some((hours, rest)) = time.split_once(':') else {
        return false;
    };
    if hours.len() > 2 {
        return false;
    }
    let Some(hour) = number(hours.as_bytes()) else {
        return false;
    };
    let rest = rest.as_bytes();
    if rest.len() < 5 || rest[2] != b':' {
        return false;
    }
    let (Some(minute), Some(second)) = (number(&rest[..2]), number(&rest[3..5])) else {
        return false;
    };
    if hour >= 24 || minute >= 60 || second >= 60 {
        return false;
    }
    let mut zone = &rest[5..];
    if zone.first().is_some_and(|c| *c == b'.' || *c == b',') {
        let digits = zone
            .iter()
            .skip(1)
            .take_while(|c| c.is_ascii_digit())
            .count();
        if digits == 0 {
            return false;
        }
        zone = &zone[digits + 1..];
    }
    if zone == b"Z" {
        return true;
    }
    if zone.len() != 6 || ![b'+', b'-'].contains(&zone[0]) || zone[3] != b':' {
        return false;
    }
    let (Some(hour), Some(minute)) = (number(&zone[1..3]), number(&zone[4..6])) else {
        return false;
    };
    hour <= 24 && minute <= 60
}
