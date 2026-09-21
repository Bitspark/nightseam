import json
import math
import unittest
from pathlib import Path

from nightseam.runtime import PublicError, Schema
from nightseam.runtime.envelope import decode_envelope
from nightseam.runtime.json import dumps, loads

TABLES = Path(__file__).resolve().parents[3] / "conformance" / "tables"


class ValidatorTests(unittest.TestCase):
    def test_shared_document_examples(self):
        table = json.loads((TABLES / "examples.json").read_text(encoding="utf8"))
        checkouts = {}
        for corpus, descriptions in table["schemas"].items():
            schemas = {}
            for name, descriptor in descriptions.items():
                schemas[name] = Schema(descriptor, "", schemas)
            checkouts[corpus] = schemas
        proof = 0
        for row in table["rows"]:
            with self.subTest(corpus=row["corpus"], family=row["family"], path=row["path"]):
                if "unavailable" in row:
                    self.assertNotIn("value", row)
                    self.assertTrue(row["unavailable"]["Reason"])
                    self.assertIn(row["unavailable"]["Kind"], ("limit", "impossible"))
                    continue
                schemas = checkouts[row["corpus"]]
                schema = schemas[row["family"]]
                slots = {
                    name: {"family": schemas[binding["Family"]]} if "Family" in binding else {"type": binding["Type"]}
                    for name, binding in row.get("bindings", {}).items()
                }
                value = row["value"]
                if row.get("to"):
                    decode_envelope(dumps(value), row["to"])
                    if row.get("member"):
                        value = value[row["member"]]
                if "expression" in row:
                    schema.validate(row["expression"], value, slots=slots)
                if row["family"] == "proof":
                    proof += 1
        self.assertGreaterEqual(proof, 30)

    def test_callable_identity_retains_its_defining_schema(self):
        descriptor = {"types": {"Call": {"kind": "callable", "contract": "worker/Call"}}}
        worker = Schema(descriptor, "a" * 64)
        consumer = Schema({"types": {"Alias": {"kind": "alias", "type": "worker.Call"}}}, "b" * 64, {"worker": worker})
        reference = {"binding": "scope.1", "contract": "worker/Call", "digest": "a" * 64}
        consumer.validate("Alias", reference)
        consumer.validate("Alias", {"binding": "scope.1", "contract": "worker/Call"})
        with self.assertRaises(PublicError) as error:
            consumer.validate("Alias", {**reference, "digest": "b" * 64})
        self.assertEqual(error.exception.code, "contract_mismatch")
        for digest in (None, "A" * 64, "a" * 63, 1):
            with self.subTest(digest=digest), self.assertRaisesRegex(ValueError, "schema.digest"):
                Schema(descriptor, digest)

    def test_shared_validator_table(self):
        table = json.loads((TABLES / "validator.json").read_text(encoding="utf8"))
        imported = {}
        for name, descriptor in table["imported"].items():
            imported[name] = Schema(descriptor, "", imported)
        schema = Schema(table["wire"], "", imported)
        for row in table["cases"]:
            with self.subTest(expression=row["expression"], value=row["value"]):
                bindings = {
                    name: {"family": imported[binding["family"]]}
                    if "family" in binding
                    else {"type": binding["type"], "schema": schema}
                    for name, binding in row.get("slots", {}).items()
                }
                message = ""
                try:
                    schema.validate(row["expression"], row["value"], slots=bindings)
                except ValueError as error:
                    message = str(error)
                self.assertEqual(not message, row["valid"], message)
                if "message" in row:
                    self.assertEqual(message, row["message"])
        for law in table["equivalence"]:
            for value in law["values"]:
                with self.subTest(law=law, value=value):
                    results = []
                    for expression in (law["generic"], law["bound"]):
                        try:
                            schema.validate(expression, value)
                            results.append(True)
                        except ValueError:
                            results.append(False)
                    self.assertEqual(*results)

    def test_shared_patterns(self):
        table = json.loads((TABLES / "validator.json").read_text(encoding="utf8"))

        def descriptor(pattern):
            return {
                "types": {
                    "Probe": {
                        "kind": "record",
                        "fields": [{"name": "text", "type": "string", "required": True, "pattern": pattern}],
                    }
                }
            }

        for row in table["patterns"]:
            with self.subTest(row=row):
                if row["valid"]:
                    Schema(descriptor(row["pattern"]), "")
                else:
                    with self.assertRaisesRegex(ValueError, "outside Nightseam dialect"):
                        Schema(descriptor(row["pattern"]), "")
        for row in table["patternValues"]:
            with self.subTest(row=row):
                schema = Schema(descriptor(row["pattern"]), "")
                if row["valid"]:
                    schema.validate("Probe", {"text": row["value"]})
                else:
                    with self.assertRaises(ValueError) as error:
                        schema.validate("Probe", {"text": row["value"]})
                    self.assertEqual(str(error.exception), "$.text: expected a match of " + row["pattern"])

    def test_shared_unicode(self):
        for row in json.loads((TABLES / "unicode.json").read_text(encoding="utf8"))["rows"]:
            with self.subTest(row=row["name"]):
                if row["valid"]:
                    value = loads(row["raw"])
                    self.assertEqual(loads(dumps(value)), value)
                else:
                    with self.assertRaisesRegex(ValueError, "Unicode scalar strings"):
                        loads(row["raw"])
        for value in ("\ud800", {"\udc00": 1}, ["\ud800"]):
            with self.subTest(value=repr(value)), self.assertRaises(ValueError):
                dumps(value)
        with self.assertRaises(ValueError):
            loads(b'"\xff"')
        for value in (math.inf, math.nan, {"x": math.inf}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                dumps(value)

    def test_shared_envelopes(self):
        for row in json.loads((TABLES / "frames.json").read_text(encoding="utf8"))["rows"]:
            roles = ("server", "client") if row["to"] == "either" else (row["to"],)
            for role in roles:
                with self.subTest(row=row["name"], role=role):
                    if row["valid"]:
                        decode_envelope(row["frame"], role)
                    else:
                        with self.assertRaises(ValueError):
                            decode_envelope(row["frame"], role)


if __name__ == "__main__":
    unittest.main()
