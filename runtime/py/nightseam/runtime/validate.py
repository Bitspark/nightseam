"""Interpret declaration descriptors with their original family and binding scope."""

import math
import re
from dataclasses import dataclass, replace

from .json import dumps, scalar_value
from .pattern import compile_pattern
from .peer import PublicError

PRIMITIVES = ("json", "string", "boolean", "number", "integer", "timestamp")


def ordered_keys(value):
    # Diagnostic selection follows the shared table's UTF-16 key order.
    return sorted(value, key=lambda key: key.encode("utf-16-be"))


def bad(location, expected):
    raise ValueError(location + ": expected " + expected)


def json_value(value, location, seen=None):
    if value is None or isinstance(value, (str, bool)):
        return
    if isinstance(value, (int, float)):
        if not math.isfinite(value):
            bad(location, "finite JSON number")
        return
    seen = set() if seen is None else seen
    if not isinstance(value, (dict, list)) or id(value) in seen:
        bad(location, "acyclic JSON")
    seen.add(id(value))
    if isinstance(value, list):
        for index, item in enumerate(value):
            json_value(item, f"{location}[{index}]", seen)
    else:
        if any(not isinstance(key, str) for key in value):
            bad(location, "plain JSON object")
        for key in ordered_keys(value):
            json_value(value[key], location + "." + key, seen)
    seen.remove(id(value))


def timestamp(value):
    match = re.fullmatch(
        r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.[0-9]+)?(?:Z|([+-])([0-9]{2}):([0-9]{2}))",
        value,
    )
    if not match:
        return False
    year, month, day, hour, minute, second = map(int, match.groups()[:6])
    leap = year % 4 == 0 and (year % 100 != 0 or year % 400 == 0)
    days = (31, 29 if leap else 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31)
    return (
        1 <= month <= 12
        and 1 <= day <= days[month - 1]
        and hour <= 23
        and minute <= 59
        and second <= 59
        and (not match[7] or int(match[8]) <= 23 and int(match[9]) <= 59)
    )


def check_patterns(value, seen=None):
    seen = set() if seen is None else seen
    if not isinstance(value, (dict, list)) or id(value) in seen:
        return
    seen.add(id(value))
    if isinstance(value, dict):
        if isinstance(value.get("pattern"), str):
            compile_pattern(value["pattern"])
        for key in ordered_keys(value):
            check_patterns(value[key], seen)
    else:
        for child in value:
            check_patterns(child, seen)


def number_text(value):
    return str(int(value)) if isinstance(value, float) and value.is_integer() else str(value)


def constrain(field, value, location):
    for name, numeric, textual, compare in (
        ("min", "at least ", "at or after ", lambda a, b: a < b),
        ("max", "at most ", "at or before ", lambda a, b: a > b),
    ):
        if name not in field:
            continue
        bound = field[name]
        if isinstance(value, (int, float)) and not isinstance(value, bool) and compare(value, bound):
            bad(location, numeric + number_text(bound))
        if isinstance(value, str) and compare(value, number_text(bound)):
            bad(location, textual + number_text(bound))
    if isinstance(value, (str, list)) and "length" in field:
        length = field["length"]
        if "min" in length and len(value) < length["min"]:
            bad(location, "a length of at least " + str(length["min"]))
        if "max" in length and len(value) > length["max"]:
            bad(location, "a length of at most " + str(length["max"]))
    if field.get("pattern") and isinstance(value, str) and not compile_pattern(field["pattern"]).matches(value):
        bad(location, "a match of " + field["pattern"])


@dataclass
class Expression:
    schema: "Schema"
    value: object
    scope: dict
    aliases: frozenset = frozenset()
    definition: dict | None = None
    name: str | None = None

    def child(self, value):
        return Expression(self.schema, value, self.scope)


def single_family_parameter(schema):
    parameters = [parameter for parameter in schema.family.get("parameters", []) if parameter.get("of")]
    return parameters[0] if len(parameters) == 1 else None


def free_parameters(schema, definition, seen=None):
    seen = set() if seen is None else seen
    if not definition or id(definition) in seen:
        return []
    seen.add(id(definition))
    used = set()

    def inherit(target):
        used.update(parameter["name"] for parameter in free_parameters(schema, target, seen))

    def walk_base(base):
        if isinstance(base, dict) and "apply" in base:
            for filler in base["with"].values():
                walk(filler)
        else:
            walk(base)

    def walk(value):
        if isinstance(value, str):
            prefix, separator, name = value.partition(".")
            if any(parameter["name"] == prefix for parameter in schema.family.get("parameters", [])):
                used.add(prefix)
            elif not separator:
                inherit(schema.family["types"].get(value))
            else:
                imported = schema.imported.get(prefix)
                target = imported.family["types"].get(name) if imported else None
                if target:
                    needed = free_parameters(imported, target, seen) + target.get("parameters", [])
                    source = single_family_parameter(schema)
                    if source and any(parameter.get("of") for parameter in needed):
                        used.add(source["name"])
        elif isinstance(value, dict):
            for wrapper in ("array", "map", "nullable"):
                if wrapper in value:
                    walk(value[wrapper])
                    return
            if "apply" in value:
                if "." not in value["apply"]:
                    inherit(schema.family["types"].get(value["apply"]))
                for filler in value["with"].values():
                    walk(filler)
            elif "ref" in value:
                entity = schema.family["types"].get(value["ref"], {})
                for field in entity.get("fields", []):
                    if field["name"] == entity.get("key"):
                        walk(field["type"])
            elif "kind" in value:
                for field in value.get("fields", []):
                    walk(field["type"])
                for base in value.get("extends", []):
                    walk_base(base)
                for variant in value.get("variants", {}).values():
                    walk(variant)

    walk(definition.get("type"))
    for field in definition.get("fields", []):
        walk(field["type"])
    for base in definition.get("extends", []):
        walk_base(base)
    for variant in definition.get("variants", {}).values():
        walk(variant)
    seen.remove(id(definition))
    return [parameter for parameter in schema.family.get("parameters", []) if parameter["name"] in used]


def named(expression, name, location):
    if "." in name:
        caller = expression
        family, name = name.split(".", 1)
        if family and "A" <= family[0] <= "Z":
            schema = expression.scope.get(family, {}).get("family")
            if schema is None:
                bad(location, "a binding of the parameter " + family)
        else:
            schema = expression.schema.imported.get(family)
            if schema is None:
                bad(location, "known family")
        expression = Expression(schema, name, {})
        if family and "a" <= family[0] <= "z":
            source = single_family_parameter(caller.schema)
            target = schema.family["types"].get(name)
            if source and target:
                binding = caller.scope.get(source["name"])
                if binding:
                    for parameter in free_parameters(schema, target) + target.get("parameters", []):
                        if parameter.get("of"):
                            expression.scope[parameter["name"]] = binding
    definition = expression.schema.family["types"].get(name)
    if definition is None:
        bad(location, "known type")
    return expression.child(name), definition, name


def resolve(expression, location, inheritance=False):
    aliases = set(expression.aliases)
    while True:
        value = expression.value
        if isinstance(value, str):
            argument = expression.scope.get(value)
            if argument:
                if "type" not in argument:
                    bad(location, "type argument")
                expression = argument["type"]
                aliases = set(expression.aliases)
                continue
            if value in PRIMITIVES:
                return expression
            expression, definition, name = named(expression, value, location)
        elif isinstance(value, dict):
            if "apply" in value:
                target, definition, name = named(expression, value["apply"], location)
                if not isinstance(value.get("with"), dict):
                    bad(location, "application arguments")
                scope = dict(target.scope)
                parameters = (
                    free_parameters(target.schema, definition) if inheritance or "." in value["apply"] else []
                ) + definition.get("parameters", [])
                allowed = set()
                for parameter in parameters:
                    key = parameter["name"]
                    allowed.add(key)
                    if key not in value["with"]:
                        bad(location, "an argument for " + key)
                    filler = value["with"][key]
                    if not parameter.get("of"):
                        scope[key] = {"type": replace(expression.child(filler), aliases=frozenset(aliases))}
                    else:
                        if not isinstance(filler, str):
                            bad(location, "family argument for " + key)
                        family = expression.scope.get(filler, {}).get("family") or expression.schema.imported.get(
                            filler
                        )
                        if family is None:
                            bad(location, "known family argument for " + key)
                        scope[key] = {"family": family}
                for key in ordered_keys(value["with"]):
                    if key not in allowed:
                        bad(location, "known parameter " + key)
                expression = replace(target, scope=scope)
            elif "kind" in value:
                definition, name = value, value["kind"]
            else:
                return expression
        else:
            bad(location, "type expression")
        inheritance = False
        if definition["kind"] == "alias":
            if id(definition) in aliases:
                bad(location, "acyclic type expression")
            aliases.add(id(definition))
            expression = expression.child(definition["type"])
            continue
        return replace(expression, definition=definition, name=name)


def inherited(expression, base, location):
    if isinstance(base, str):
        owner, definition, _ = named(expression, base, location)
        if definition.get("parameters") or free_parameters(owner.schema, definition):
            bad(location, "explicit application of generic base " + base)
    return resolve(expression.child(base), location, True)


def fields(expression, location, seen=None):
    definition = expression.definition
    if definition is None:
        bad(location, "record")
    seen = set() if seen is None else seen
    if id(definition) in seen:
        bad(location, "acyclic inheritance")
    seen.add(id(definition))
    result = []
    for base in definition.get("extends", []):
        result.extend(fields(inherited(expression, base, location), location, seen))
    result.extend((field, expression.child(field["type"])) for field in definition.get("fields", []))
    seen.remove(id(definition))
    return result


def variants(expression, location, seen=None):
    definition = expression.definition
    if definition is None or definition["kind"] != "union":
        bad(location, "union")
    seen = set() if seen is None else seen
    if id(definition) in seen:
        bad(location, "acyclic inheritance")
    seen.add(id(definition))
    result = {}
    for base in definition.get("extends", []):
        result.update(variants(inherited(expression, base, location), location, seen))
    result.update((tag, expression.child(variant)) for tag, variant in definition.get("variants", {}).items())
    seen.remove(id(definition))
    return result


def validate(expression, value, location):
    resolved = resolve(expression, location)
    definition = resolved.definition
    if definition is not None:
        kind = definition["kind"]
        if kind == "callable":
            contract = definition["contract"]
            if not isinstance(value, dict):
                bad(location, "a live reference to " + contract)
            if not isinstance(value.get("binding"), str) or not value["binding"]:
                raise ValueError(location + ".binding: a live reference names the binding it refers to")
            if not isinstance(value.get("contract"), str):
                raise ValueError(location + ".contract: a live reference carries the declaration it implements")
            if value["contract"] != contract:
                raise ValueError(
                    location
                    + ".contract: the reference carries "
                    + value["contract"]
                    + " where "
                    + contract
                    + " is expected"
                )
            digest = value.get("digest", "")
            if "digest" in value and (not isinstance(digest, str) or not re.fullmatch("[0-9a-f]{64}", digest)):
                raise ValueError(location + ".digest: expected lowercase SHA-256 digest")
            if digest and resolved.schema.digest and digest != resolved.schema.digest:
                raise PublicError(
                    "contract_mismatch",
                    location
                    + ".digest: the reference to "
                    + contract
                    + " carries declaration digest "
                    + digest
                    + " where "
                    + resolved.schema.digest
                    + " is expected",
                )
            for key in ordered_keys(value):
                if key not in ("binding", "contract", "digest"):
                    raise ValueError(location + "." + key + ": unknown field")
        elif kind == "enum":
            if not isinstance(value, str) or value not in definition.get("values", []):
                bad(location, resolved.name)
        elif kind in ("record", "entity"):
            if not isinstance(value, dict):
                bad(location, resolved.name + " object")
            allowed = set()
            for field, scoped in fields(resolved, location):
                key = field["name"]
                allowed.add(key)
                at = location + "." + key
                if key not in value:
                    if field.get("required"):
                        raise ValueError(at + ": required field missing")
                    continue
                member = value[key]
                if member is None:
                    if field.get("nullable"):
                        continue
                    nullable = resolve(scoped, at).value
                    if not isinstance(nullable, dict) or "nullable" not in nullable:
                        raise ValueError(at + ": null is not permitted")
                validate(scoped, member, at)
                if member is not None:
                    constrain(field, member, at)
            for key in ordered_keys(value):
                if key in allowed:
                    continue
                if not definition.get("open"):
                    raise ValueError(location + "." + key + ": unknown field")
                json_value(value[key], location + "." + key)
        elif kind == "union":
            if not isinstance(value, dict):
                bad(location, resolved.name + " object")
            tag = definition["tag"]
            if tag not in value:
                raise ValueError(location + "." + tag + ": required field missing")
            choices = variants(resolved, location)
            if not isinstance(value[tag], str) or value[tag] not in choices:
                bad(location + "." + tag, "known variant")
            variant = choices[value[tag]]
            member = definition.get("value", "value")
            empty = variant.value == {"empty": True}
            if not empty:
                if member not in value:
                    raise ValueError(location + "." + member + ": required field missing")
                validate(variant, value[member], location + "." + member)
            for key in ordered_keys(value):
                if key != tag and (empty or key != member):
                    raise ValueError(location + "." + key + ": unknown field")
        else:
            bad(location, "supported type")
        return
    expression_type = resolved.value
    if isinstance(expression_type, dict):
        if "nullable" in expression_type:
            if value is not None:
                validate(resolved.child(expression_type["nullable"]), value, location)
        elif "literal" in expression_type:
            literal = expression_type["literal"]
            if not isinstance(literal, str) or not literal:
                bad(location, "nonempty string literal")
            if value != literal:
                quoted = dumps(literal)
                for char in "<>&\u2028\u2029":
                    quoted = quoted.replace(char, "\\u" + format(ord(char), "04x"))
                bad(location, "literal " + quoted)
        elif "array" in expression_type:
            if not isinstance(value, list):
                bad(location, "array")
            for index, item in enumerate(value):
                validate(resolved.child(expression_type["array"]), item, f"{location}[{index}]")
        elif "map" in expression_type:
            if not isinstance(value, dict):
                bad(location, "object")
            for key in ordered_keys(value):
                validate(resolved.child(expression_type["map"]), value[key], location + "." + key)
        elif "ref" in expression_type:
            try:
                target, entity, _ = named(resolved, expression_type["ref"], location)
            except ValueError:
                bad(location, "known entity")
            key = next(
                (
                    scoped
                    for field, scoped in fields(resolve(target, location), location)
                    if field["name"] == entity.get("key")
                ),
                None,
            )
            if key is None:
                bad(location, "entity with a key")
            validate(key, value, location)
        elif "empty" in expression_type:
            if not isinstance(value, dict) or value:
                bad(location, "empty object")
        else:
            bad(location, "supported type expression")
    elif expression_type == "json":
        json_value(value, location)
    elif expression_type == "string":
        if not isinstance(value, str):
            bad(location, "string")
    elif expression_type == "boolean":
        if not isinstance(value, bool):
            bad(location, "boolean")
    elif expression_type in ("number", "integer"):
        if not isinstance(value, (int, float)) or isinstance(value, bool):
            bad(location, expression_type)
        if not math.isfinite(value):
            bad(location, "finite number")
        if expression_type == "integer" and (value % 1 != 0 or abs(value) > 9007199254740991):
            bad(location, "JavaScript-safe integer")
    elif expression_type == "timestamp":
        if not isinstance(value, str):
            bad(location, "timestamp")
        if not timestamp(value):
            bad(location, "RFC3339 timestamp")
    else:
        bad(location, "known type")


class Schema:
    """A family descriptor and imports, retained for scoped applications."""

    def __init__(self, family, digest, imported=None):
        if not isinstance(digest, str) or (digest and not re.fullmatch("[0-9a-f]{64}", digest)):
            raise ValueError("schema.digest: expected empty or lowercase SHA-256 digest")
        self.digest = digest
        scalar_value(family)
        self.imported = {} if imported is None else imported
        scalar_value(list(self.imported))
        if not isinstance(family, dict) or "types" not in family:
            raise ValueError("expected family descriptor with types")
        check_patterns(family["types"])
        self.family = family

    def validate(self, expression, value, location="$", slots=None):
        scalar_value(expression)
        scalar_value(value)
        check_patterns(expression)
        scope = {}
        for name, binding in (slots or {}).items():
            scalar_value(name)
            if "type" in binding:
                scalar_value(binding["type"])
                check_patterns(binding["type"])
                scope[name] = {"type": Expression(binding.get("schema", self), binding["type"], {})}
            else:
                scope[name] = {"family": binding["family"]}
        validate(Expression(self, expression, scope), value, location)
