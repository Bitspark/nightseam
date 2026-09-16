package generate

import (
	"fmt"
	"path"
	"strings"
)

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
func generateTS(api API, options Options) (map[string][]byte, error) {
	files := map[string][]byte{}
	var types strings.Builder
	types.WriteString(generatedHeader)
	for _, name := range sortedTypeNames(api) {
		t := api.Types[name]
		switch t.Kind {
		case "record":
			fmt.Fprintf(&types, "export interface %s", name)
			types.WriteString(" {\n")
			fields := flattenedAPIFields(api, name)
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
	types.WriteString(tsValidationTemplate)
	files[path.Join(options.TSClientPath, "src/types.ts")] = []byte(types.String())
	var client strings.Builder
	client.WriteString(generatedHeader)
	client.WriteString("import { DuplexPeer, DuplexError, type PeerOptions, type CallOptions, type RequestContext } from '@nighthall/ws-runtime';\nimport { validateWire } from './types.ts';\nimport type * as Protocol from './types.ts';\nexport * from './types.ts';\nexport { DuplexError };\nexport interface Handler {\n")
	for _, m := range api.Methods {
		if m.Direction == "server_to_client" {
			fmt.Fprintf(&client, "  %s(params: %s, context: RequestContext): %s | Promise<%s>;\n", m.TSName, tsRequest(m), tsQualified(m.Result), tsQualified(m.Result))
		}
	}
	client.WriteString("}\n")
	client.WriteString("export class Client {\n  readonly peer: DuplexPeer;\n  constructor(peer: DuplexPeer, handler?: Handler) {\n    this.peer = peer;\n")
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
	files[path.Join(options.TSClientPath, "src/index.ts")] = []byte(client.String())
	manifest := map[string]any{"name": "@nighthall/" + api.Name + "-client", "version": "0.0.0", "private": true, "type": "module", "exports": "./src/index.ts", "scripts": map[string]any{"check": "tsc --noEmit"}, "dependencies": map[string]any{"@nighthall/ws-runtime": "0.1.0"}}
	files[path.Join(options.TSClientPath, "package.json")] = append(canonicalJSON(manifest), '\n')
	files[path.Join(options.TSClientPath, "tsconfig.json")] = []byte("{\"compilerOptions\":{\"target\":\"ES2022\",\"module\":\"NodeNext\",\"moduleResolution\":\"NodeNext\",\"strict\":true,\"skipLibCheck\":true,\"noEmit\":true,\"allowImportingTsExtensions\":true,\"lib\":[\"ES2022\",\"DOM\"]},\"include\":[\"src/**/*.ts\"]}\n")
	return files, nil
}
func upperFirst(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
func tsRequest(m Method) string {
	if m.Request == "" {
		return "Record<string, never>"
	}
	return "Protocol." + m.Request
}
func tsRequestExpr(m Method) string {
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
