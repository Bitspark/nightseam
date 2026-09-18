// Package typescript renders an API family as a TypeScript client package:
// the wire types with their validator, the client, a manifest and a compiler
// configuration. It implements spi.Language and is named nowhere but where
// the tool is composed.
//
// It descends from Nightshift's dev/go/repo/generate/typescript.go and the
// TypeScript checks of its contract.go (D-001).
package typescript

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/contract"
	"github.com/Bitspark/nighthall/tools/go/generate-api/internal/spi"
)

// Options places the generated package. A path left empty is derived from
// the family's name when a contract is rendered.
type Options struct {
	ClientPath string
}

// New returns the TypeScript language with its options.
func New(options Options) spi.Language { return &language{options} }

type language struct{ options Options }

func (*language) Name() string { return "typescript" }

func (l *language) resolve(api contract.API) (string, error) {
	p := l.options.ClientPath
	if p == "" {
		p = "api/ts/" + api.Name + "-client"
	}
	if p == "." || p == ".." || strings.Contains(p, "\\") || strings.Contains(p, ":") || strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.HasPrefix(p, "../") {
		return "", fmt.Errorf("invalid output path %q", p)
	}
	return p, nil
}

// Render emits the four files of the client package.
func (l *language) Render(api contract.API) ([]spi.File, error) {
	dir, err := l.resolve(api)
	if err != nil {
		return nil, err
	}
	return generateTS(api, dir), nil
}

// reservedTypes are the identifiers the generated client declares or would
// shadow in the module that declares the contract's types; a contract type
// of that name is refused.
var reservedTypes = strings.Fields("Client Caller Handler TypeExpression WireType WireField Array Record Promise AbortSignal Date Number Object Set Error")

// A method named then would also make the client a Promise-like value, breaking
// the async dial factory through JavaScript's thenable assimilation.
var reservedMethods = strings.Fields("constructor close call notify handle connect then")
var reservedWords = strings.Fields("break case catch class const continue debugger default delete do else enum export extends false finally for function if import in instanceof new null return super switch this throw true try typeof var void while with as implements interface let package private protected public static yield any boolean number string symbol type from of namespace unknown never object")

// Check reports the names the contract would make TypeScript generate that
// it cannot: names the client reserves or the language does, and collisions
// among the client's members.
func (*language) Check(api contract.API) []contract.Diagnostic {
	diagnostics := []contract.Diagnostic{}
	add := func(code, pointer, message string) {
		diagnostics = append(diagnostics, contract.Diagnostic{Code: code, Pointer: pointer, Message: message})
	}
	for _, name := range api.TypeNames() {
		if slices.Contains(reservedTypes, name) {
			add("reserved_name", "/types/"+contract.EscapePointer(name), "Type name is reserved by the generated TypeScript client: "+name+".")
		}
	}
	// The TypeScript namespace is shared across methods and events in each direction.
	operations := map[string]string{}
	operation := func(kind string, index int, tsName, direction string) {
		p := fmt.Sprintf("/%s/%d", kind, index)
		key := direction + ":" + tsName
		if previous, ok := operations[key]; ok {
			add("operation_collision", p+"/ts_name", "Operation name collides with "+previous+".")
		} else {
			operations[key] = p
		}
		if slices.Contains(reservedMethods, tsName) {
			add("reserved_name", p+"/ts_name", "Operation name is reserved by the generated TypeScript client.")
		}
		if slices.Contains(reservedWords, tsName) {
			add("reserved_name", p+"/ts_name", "Operation name is a reserved TypeScript identifier.")
		}
	}
	for i, method := range api.Methods {
		operation("methods", i, method.TSName, method.Direction)
	}
	for i, event := range api.Events {
		operation("events", i, event.TSName, event.Direction)
	}
	// Events add receive helpers as well as emit helpers, so their receiver
	// namespace crosses the protocol's direction boundary.
	client := map[string]string{"peer": "generated client field", "close": "generated client method", "constructor": "generated client constructor"}
	register := func(name, pointer string) {
		if previous, exists := client[name]; exists {
			add("generated_name_collision", pointer, "Generated member "+name+" collides with "+previous+".")
		} else {
			client[name] = pointer
		}
	}
	for i, method := range api.Methods {
		if method.Direction == "client_to_server" {
			register(method.TSName, fmt.Sprintf("/methods/%d/ts_name", i))
		}
	}
	for i, event := range api.Events {
		p := fmt.Sprintf("/events/%d/ts_name", i)
		if event.Direction == "client_to_server" {
			register("emit"+upperFirst(event.TSName), p)
		} else {
			register("on"+upperFirst(event.TSName), p)
		}
	}
	return diagnostics
}

// tsAlias is the namespace a generated file refers to an imported family's
// types by: the family's name without its dashes.
func tsAlias(family string) string { return strings.ReplaceAll(family, "-", "") }

// tsPackage is the package an imported family's client is published as.
func tsPackage(family string) string { return "@nighthall/" + family + "-client" }

// tsImports is the import lines a generated file needs for the families the
// contract imports: their types as a namespace and, where asked, their
// validator under a name no type can collide with.
func tsImports(api contract.API, validators bool) string {
	var b strings.Builder
	for _, family := range api.Imports {
		fmt.Fprintf(&b, "import type * as %s from %s;\n", tsAlias(family), quote(tsPackage(family)))
		if validators {
			fmt.Fprintf(&b, "import { validateWire as validate_%s } from %s;\n", tsAlias(family), quote(tsPackage(family)))
		}
	}
	return b.String()
}

func quote(value string) string      { encoded, _ := json.Marshal(value); return string(encoded) }
func expression(value any) string    { encoded, _ := json.Marshal(value); return string(encoded) }
func canonicalJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func tsType(expression any) string {
	switch t := expression.(type) {
	case string:
		switch t {
		case "string", "boolean":
			return t
		case "number", "integer":
			return "number"
		case "timestamp":
			return "string"
		case "json":
			return "unknown"
		default:
			if family, name, ok := contract.Reference(t); ok {
				return tsAlias(family) + "." + name
			}
			return t
		}
	case map[string]any:
		if item, ok := t["array"]; ok {
			return "Array<" + tsType(item) + ">"
		}
		if item, ok := t["map"]; ok {
			return "Record<string, " + tsType(item) + ">"
		}
	}
	return "unknown"
}

// generateTS renders the four files of the client package into dir.
func generateTS(api contract.API, dir string) []spi.File {
	var files []spi.File
	var types strings.Builder
	types.WriteString(spi.Header)
	types.WriteString(tsImports(api, true))
	for _, name := range api.TypeNames() {
		t := api.Types[name]
		switch t.Kind {
		case "record":
			fmt.Fprintf(&types, "export interface %s", name)
			types.WriteString(" {\n")
			fields := api.FlattenedFields(name)
			for _, field := range fields {
				optional := ""
				if !field.Required {
					optional = "?"
				}
				null := ""
				if field.Nullable {
					null = " | null"
				}
				fmt.Fprintf(&types, "  %s%s: %s%s;\n", quote(field.Name), optional, tsType(field.Type), null)
			}
			if t.Open {
				types.WriteString("  [key: string]: unknown;\n")
			}
			types.WriteString("}\n")
		case "enum":
			values := make([]string, len(t.Values))
			for i, v := range t.Values {
				values[i] = quote(v)
			}
			fmt.Fprintf(&types, "export type %s = %s;\n", name, strings.Join(values, " | "))
		case "alias":
			fmt.Fprintf(&types, "export type %s = %s;\n", name, tsType(t.Type))
		}
	}
	types.WriteString("\nconst contractTypes = ")
	types.Write(canonicalJSON(api.Types))
	types.WriteString(" as unknown as Record<string, WireType>;\n")
	types.WriteString("const importedValidators: Record<string, (type: TypeExpression, value: unknown, location?: string) => void> = {")
	for i, family := range api.Imports {
		if i > 0 {
			types.WriteString(", ")
		}
		fmt.Fprintf(&types, "%s: validate_%s", quote(family), tsAlias(family))
	}
	types.WriteString("};\n")
	types.WriteString(tsValidationTemplate)
	files = append(files, spi.File{Path: path.Join(dir, "src/types.ts"), Data: []byte(types.String())})
	var client strings.Builder
	client.WriteString(spi.Header)
	client.WriteString("import { DuplexPeer, DuplexError, type PeerOptions, type CallOptions, type RequestContext } from '@nighthall/ws-runtime';\nimport { validateWire } from './types.ts';\nimport type * as Protocol from './types.ts';\n" + tsImports(api, false) + "export * from './types.ts';\nexport { DuplexError };\nexport interface Handler {\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			fmt.Fprintf(&client, "  %s(params: %s, context: RequestContext): %s | Promise<%s>;\n", m.TSName, tsRequest(m), tsQualified(m.Result), tsQualified(m.Result))
		}
	}
	client.WriteString("}\n")
	// The RPC layer's caller side as an interface, which the session class
	// implements; a consumer may stand another implementation in its place.
	client.WriteString("export interface Caller {\n")
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			parameters := "params: " + tsRequest(m) + ", options?: CallOptions"
			if m.Request == "" {
				parameters = "options?: CallOptions"
			}
			fmt.Fprintf(&client, "  %s(%s): Promise<%s>;\n", m.TSName, parameters, tsQualified(m.Result))
		}
	}
	client.WriteString("}\n")
	if s := api.Session; s != nil {
		// The sess layer's governance, as data both halves read.
		fmt.Fprintf(&client, "/** The methods that need control to send. */\nexport const decides: ReadonlySet<string> = new Set(%s);\n", expression(orEmpty(s.Decides)))
		fmt.Fprintf(&client, "/** The server-to-client methods that raise a request the holder of control must answer. */\nexport const asks: ReadonlySet<string> = new Set(%s);\n", expression(orEmpty(s.Asks)))
		if s.Conversation != nil {
			fmt.Fprintf(&client, "/** Where the agent's own conversation id arrives: the event, and the path to the id in its data. */\nexport const conversation = { event: %s, path: %s } as const;\n", quote(s.Conversation.Event), quote(s.Conversation.Path))
		}
	}
	client.WriteString("export class Client implements Caller {\n  readonly peer: DuplexPeer;\n  constructor(peer: DuplexPeer, handler?: Handler) {\n    this.peer = peer;\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			fmt.Fprintf(&client, "    if (!handler) throw new Error('reverse-call handler is required');\n    peer.handle(%q, async (params, context) => { try { validateWire(%s, params); } catch(error) { throw new DuplexError('invalid_params', String(error)); } const result = await handler.%s(params as %s, context); validateWire(%s, result); return result; });\n", m.Name, tsRequestExpr(m), m.TSName, tsRequest(m), expression(m.Result))
		}
	}
	client.WriteString("  }\n  static async dial(url: string, options: PeerOptions = {}, handler?: Handler): Promise<Client> { const peer = new DuplexPeer(options); const client = new Client(peer, handler); await peer.connect(url); return client; }\n  close(): void { this.peer.close(); }\n")
	for _, m := range api.Methods {
		if m.Direction == "client_to_server" {
			parameters := "params: " + tsRequest(m) + ", options?: CallOptions"
			initial := ""
			if m.Request == "" {
				parameters = "options?: CallOptions"
				initial = "const params = {}; "
			}
			fmt.Fprintf(&client, "  async %s(%s): Promise<%s> { %svalidateWire(%s, params); const result = await this.peer.call<%s>(%q, params, options); validateWire(%s, result); return result; }\n", m.TSName, parameters, tsQualified(m.Result), initial, tsRequestExpr(m), tsQualified(m.Result), m.Name, expression(m.Result))
		}
	}
	for _, event := range api.Events {
		if event.Direction == "client_to_server" {
			fmt.Fprintf(&client, "  async emit%s(data: %s): Promise<void> { validateWire(%s, data); await this.peer.emit(%q, data); }\n", upperFirst(event.TSName), tsQualified(event.Type), expression(event.Type), event.Name)
		} else {
			fmt.Fprintf(&client, "  on%s(handler: (data: %s) => void | Promise<void>): () => void { return this.peer.onEvent(%q, (data) => { try { validateWire(%s, data); } catch(error) { this.peer.close(); throw error; } return handler(data as %s); }); }\n", upperFirst(event.TSName), tsQualified(event.Type), event.Name, expression(event.Type), tsQualified(event.Type))
		}
	}
	client.WriteString("}\n")
	files = append(files, spi.File{Path: path.Join(dir, "src/index.ts"), Data: []byte(client.String())})
	dependencies := map[string]any{"@nighthall/ws-runtime": "0.1.0"}
	for _, family := range api.Imports {
		dependencies[tsPackage(family)] = "0.0.0"
	}
	manifest := map[string]any{"name": "@nighthall/" + api.Name + "-client", "version": "0.0.0", "private": true, "type": "module", "exports": "./src/index.ts", "scripts": map[string]any{"check": "tsc --noEmit"}, "dependencies": dependencies}
	files = append(files, spi.File{Path: path.Join(dir, "package.json"), Data: append(canonicalJSON(manifest), '\n')})
	files = append(files, spi.File{Path: path.Join(dir, "tsconfig.json"), Data: []byte("{\"compilerOptions\":{\"target\":\"ES2022\",\"module\":\"NodeNext\",\"moduleResolution\":\"NodeNext\",\"strict\":true,\"skipLibCheck\":true,\"noEmit\":true,\"allowImportingTsExtensions\":true,\"lib\":[\"ES2022\",\"DOM\"]},\"include\":[\"src/**/*.ts\"]}\n")})
	return files
}

// orEmpty renders an absent list as an empty one.
func orEmpty(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
func tsRequest(m contract.Method) string {
	if m.Request == "" {
		return "Record<string, never>"
	}
	return "Protocol." + m.Request
}
func tsRequestExpr(m contract.Method) string {
	if m.Request == "" {
		return "{ empty: true }"
	}
	return quote(m.Request)
}
func tsQualified(expr any) string {
	switch t := expr.(type) {
	case string:
		switch t {
		case "string", "boolean", "number", "integer", "timestamp", "json":
			return tsType(t)
		default:
			if family, name, ok := contract.Reference(t); ok {
				return tsAlias(family) + "." + name
			}
			return "Protocol." + t
		}
	case map[string]any:
		if item, ok := t["array"]; ok {
			return "Array<" + tsQualified(item) + ">"
		}
		if item, ok := t["map"]; ok {
			return "Record<string, " + tsQualified(item) + ">"
		}
	}
	return "unknown"
}

const tsValidationTemplate = `
type TypeExpression = string | {array: TypeExpression} | {map: TypeExpression} | {empty: true};
interface WireField {name: string;type: TypeExpression;required?: boolean;nullable?: boolean}
interface WireType {kind: string;fields?: WireField[];extends?: string[];open?: boolean;values?: string[];type?: TypeExpression}
function fields(name: string): WireField[] {const type = contractTypes[name]!; return [...(type.extends ?? []).flatMap(fields), ...(type.fields ?? [])];}
function timestamp(value: unknown): boolean {if(typeof value!=='string')return false;const m=/^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|([+-])(\d\d):(\d\d))$/.exec(value);if(!m)return false;const year=Number(m[1]),month=Number(m[2]),day=Number(m[3]);const leap=year%4===0&&(year%100!==0||year%400===0);const days=[31,leap?29:28,31,30,31,30,31,31,30,31,30,31];return month>=1&&month<=12&&day>=1&&day<=days[month-1]!&&Number(m[4])<=23&&Number(m[5])<=59&&Number(m[6])<=59&&(!m[7]||(Number(m[8])<=23&&Number(m[9])<=59))&&!Number.isNaN(Date.parse(value));}
function jsonValue(value: unknown, seen = new Set<object>()): void {if (value === null || typeof value === 'string' || typeof value === 'boolean') return; if (typeof value === 'number' && Number.isFinite(value)) return; if (typeof value !== 'object' || seen.has(value)) throw new Error('expected finite acyclic JSON'); seen.add(value); if (Array.isArray(value)) for (const child of value) jsonValue(child,seen); else {if(Object.getPrototypeOf(value)!==Object.prototype && Object.getPrototypeOf(value)!==null)throw new Error('expected plain JSON object'); for(const child of Object.values(value))jsonValue(child,seen);} seen.delete(value);}
/** Runtime validation applies equally to calls, replies, reverse calls and events. */
export function validateWire(type: TypeExpression, value: unknown, location = '$'): void {
 const bad = (expected: string): never => {throw new Error(location + ': expected ' + expected);};
 if(typeof type === 'object') {
  if('array' in type) {if(!Array.isArray(value))bad('array'); let index=0;for(const item of value as unknown[])validateWire(type.array,item,location+'['+(index++)+']');return;}
  if(value===null||typeof value!=='object'||Array.isArray(value)||(Object.getPrototypeOf(value)!==Object.prototype&&Object.getPrototypeOf(value)!==null))bad('plain object');
  if('map' in type){for(const [key,item]of Object.entries(value as Record<string,unknown>))validateWire(type.map,item,location+'.'+key);return;}
  if(Object.keys(value as object).length!==0)bad('empty object');return;
 }
 if(typeof type==='string'&&type.includes('.')){const at=type.indexOf('.');const validate=importedValidators[type.slice(0,at)];if(!validate)bad('known family');validate!(type.slice(at+1),value,location);return;}
 switch(type){
 case 'json':jsonValue(value);return;
 case 'string':if(typeof value!=='string')bad('string');return;
 case 'boolean':if(typeof value!=='boolean')bad('boolean');return;
 case 'number':if(typeof value!=='number'||!Number.isFinite(value))bad('finite number');return;
 case 'integer':if(typeof value!=='number'||!Number.isSafeInteger(value))bad('JavaScript-safe integer');return;
 case 'timestamp':if(!timestamp(value))bad('RFC3339 timestamp');return;
 }
 const definition = contractTypes[type];if(!definition)bad('known type');
 if(definition.kind==='alias'){validateWire(definition.type!,value,location);return;}
 if(definition.kind==='enum'){if(typeof value!=='string'||!definition.values!.includes(value))bad(type);return;}
 if(definition.kind!=='record')bad('supported type');
 if(value===null||typeof value!=='object'||Array.isArray(value)||(Object.getPrototypeOf(value)!==Object.prototype&&Object.getPrototypeOf(value)!==null))bad(type+' plain object');
 const object = value as Record<string,unknown>, allowed = new Set<string>();
 for(const field of fields(type)){allowed.add(field.name);if(!Object.hasOwn(object,field.name)){if(field.required!==false)bad('required field '+field.name);continue;} const child=object[field.name];if(child===null&&field.nullable)continue;if(child===null)bad('non-null field '+field.name);validateWire(field.type,child,location+'.'+field.name);}
 for(const key of Object.keys(object)){if(!allowed.has(key)){if(!definition.open)bad('known field '+key);jsonValue(object[key]);}}
}
`
