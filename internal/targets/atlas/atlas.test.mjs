import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  buildAtlas, familyView, finder, route, parseRoute, annotate, typeText,
  shapeKey, escapeHTML,
} from "./atlas.mjs";

const scalar = { Name: "Text", Kind: "alias", Alias: "string", Example: "hello" };
const checkout = { Families: [{
  Name: "alpha", Types: [scalar, {
    Name: "Node", Kind: "record", Open: true, Example: { text: "hello", next: null },
    Fields: [
      { Name: "text", Declared: "Text", Required: true, Description: "Human text." },
      { Name: "next", Declared: { nullable: "Node" }, Required: false },
    ], UsedBy: [{ Side: "server", Kind: "method", Name: "read", At: "result" }],
  }, {
    Name: "Choice", Kind: "union", Tag: "kind", Value: "body", Variants: [
      { Tag: "empty", Empty: true, Example: { kind: "empty" } },
      { Tag: "node", Declared: "Node", Example: { kind: "node", body: { text: "hello", next: null } } },
      { Tag: "record", Declared: { kind: "record", fields: [] }, Example: { kind: "record", body: {} } },
    ], Example: { kind: "empty" },
  }], Server: { Methods: [{ Name: "read", DeclaredResult: "Node", Weight: { Request: 0, Result: 2 },
    Frames: { Request: { method: "read" }, Response: { result: { text: "hello" } } } }], Events: [] },
  Client: { Methods: [], Events: [{ Name: "changed", Declared: "Node", Weight: 2, Frame: { event: "changed" } }] },
  Errors: [{ Code: "missing", Description: "Not here." }],
}, { Name: "beta", Types: [{ ...scalar, Description: "Another description." }], Errors: [{ Code: "missing" }] }] };

test("every finder route resolves to a family-view anchor, with distinct sides and kinds", () => {
  const atlas = buildAtlas(checkout);
  const ids = new Set(atlas.families.flatMap((f) => familyView(atlas, f.Name).ids));
  for (const entry of finder(atlas)) {
    assert(ids.has(entry.id), entry.id);
    assert.equal(route(...parseRoute(entry.href)), entry.href);
  }
  assert.notEqual(route("alpha", "method", "server", "same"), route("alpha", "method", "client", "same"));
  assert.equal(parseRoute("#/%ZZ"), null);
  assert.deepEqual(parseRoute(route("a b", "type", "A/B#C")), ["a b", "type", "A/B#C"]);
});

test("exchange direction follows the initiator, and weights come from the document", () => {
  const view = familyView(buildAtlas(checkout), "alpha");
  assert.deepEqual(view.exchanges.map((x) => [x.name, x.initiator, x.weight]), [["read", "client", 2], ["changed", "server", 2]]);
  assert.deepEqual(view.exchanges[0].frames, checkout.Families[0].Server.Methods[0].Frames);
  assert.deepEqual(finder(buildAtlas(checkout), "not here").map((x) => x.name), ["missing"]);
});

test("annotations retain presence and descriptions, fold recursion, and expose open records", () => {
  const atlas = buildAtlas(checkout);
  const row = annotate(atlas, "alpha", "Node", checkout.Families[0].Types[1].Example);
  assert.equal(row.children[0].presence, "required");
  assert.equal(row.children[0].description, "Human text.");
  assert.equal(row.children[1].presence, "optional");
  assert.equal(row.children[1].recursive, true);
  assert.equal(row.children.at(-1).ghost, true);
  assert.equal(row.children[1].value, null);
});

test("every union arm keeps its canonical example and the configured payload member", () => {
  const atlas = buildAtlas(checkout);
  const row = annotate(atlas, "alpha", "Choice", checkout.Families[0].Types[2].Example);
  assert.deepEqual(row.variants.map((v) => v.tag), ["empty", "node", "record"]);
  assert.equal(row.variants[0].children.length, 1);
  assert.equal(row.variants[1].children[1].name, "body");
  assert.deepEqual(row.variants[2].children[1].value, {});
});

test("type spellings describe all expression forms without language knowledge", () => {
  assert.equal(typeText({ apply: "Page", with: { T: { array: { nullable: "string" } } } }), "Page<T = (string | null)[]>");
  assert.equal(typeText({ map: { literal: "x" } }), 'map<string, "x">');
  assert.equal(typeText({ ref: "Node" }), "key of Node");
  assert.equal(typeText("S.Envelope"), "S.Envelope");
});

test("shape comparison ignores prose and wire type names, but holds constraints and presence", () => {
  const atlas = buildAtlas(checkout);
  assert.equal(shapeKey(atlas, "alpha", "Text"), shapeKey(atlas, "beta", "Text"));
  const changed = structuredClone(checkout);
  changed.Families[1].Types[0].Alias = "integer";
  assert.notEqual(shapeKey(buildAtlas(changed), "alpha", "Text"), shapeKey(buildAtlas(changed), "beta", "Text"));
  assert.equal(atlas.shared.length, 1);
  assert.equal(atlas.shared[0].same, true);
});

test("a lens never changes routes, payloads, ordering, or weights", () => {
  const atlas = buildAtlas(checkout);
  assert.deepEqual(familyView(atlas, "alpha", "unavailable"), familyView(atlas, "alpha", "wire"));
  assert.equal(escapeHTML('<script a="x">&\''), "&lt;script a=&quot;x&quot;&gt;&amp;&#39;");
});

test("the generator's proof document annotates settled forms without losing examples", { skip: !process.env.ATLAS_PROOF }, () => {
  const doc = JSON.parse(readFileSync(process.env.ATLAS_PROOF, "utf8"));
  const atlas = buildAtlas(doc);
  const f = atlas.families.find((f) => f.Name === "proof");
  assert(f);
  const part = f.Types.find((t) => t.Name === "Part");
  const annotated = annotate(atlas, f.Name, part.Name, part.Example);
  assert.equal(annotated.variants.length, part.Variants.length);
  for (const v of annotated.variants) assert(v.example !== undefined, v.tag);
  const page = f.Types.find((t) => t.Name === "Page");
  assert(annotate(atlas, f.Name, page.Name, page.Example).children.some((r) => r.type.includes("null")));
  assert(f.Types.some((t) => t.Inline));
  const parts = f.Types.find((t) => t.Name === "Parts");
  assert.equal(annotate(atlas, f.Name, parts.Name, parts.Example).children[0].children[0].kind, "union");
  const ids = new Set(atlas.families.flatMap((f) => familyView(atlas, f.Name).ids));
  for (const entry of finder(atlas)) assert(ids.has(entry.id), entry.id);
});
