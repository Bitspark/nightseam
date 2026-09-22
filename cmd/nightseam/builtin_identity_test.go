package main

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

func builtinIdentityWorld() analysis.World {
	world := analysis.World{}
	for name, family := range builtin.Families() {
		world[name] = family
	}
	world["consumer"] = modeltest.Family("consumer", map[string]string{
		"model.json":    `{"nightseam":2,"types":{"Input":{"kind":"record","fields":[{"name":"value","type":"integer"}]}}}`,
		"protocol.json": modeltest.Protocol(`"server":{"methods":{"echo":{"request":"Input","result":"integer"}}},"client":{"methods":{"echo":{"request":"Input","result":"integer"}}}`),
	})
	return world
}

// Bootstrap vocabulary remains an ordinary declaration, but its implementation
// is the runtime's responder. Emitting a second application-model constructor
// would register its declared identity.check over that same responder.
func TestBuiltinIdentityBootstrapSurface(t *testing.T) {
	world := builtinIdentityWorld()
	family := render.Build(analysis.Resolve(world, "identity"))
	if !family.HasProtocol() || family.HasModel() || !strings.Contains(family.Declaration, "identity.check") || builtin.Namespaces()["identity"] != "identity" {
		t.Fatal("bootstrap identity lost its ordinary protocol declaration or namespace")
	}
	for _, name := range []string{"consumer", "tunnel", "live"} {
		if !render.Build(analysis.Resolve(world, name)).HasModel() {
			t.Fatalf("%s lost its application-model surface", name)
		}
	}
	for _, target := range []spi.Target{golang.New(golang.Config{Module: "example.test/generated"}), typescript.New(typescript.Config{Scope: "@example"})} {
		files, err := target.Render(family)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.Contains(file.Path, "identity-binding/") || strings.HasPrefix(file.Path, "api/go/identity-client/") {
				t.Fatalf("bootstrap application adapter emitted: %s", file.Path)
			}
			for _, name := range []string{"ServerModel", "ClientModel", "func ToWire", "func FromWire", "func PrepareFromWire", "function toWire", "function fromWire", "function prepareFromWire"} {
				if strings.Contains(string(file.Data), name) {
					t.Fatalf("bootstrap %s retained %s", file.Path, name)
				}
			}
		}
		stubs, err := target.(spi.Scaffolder).Scaffold(family, "impl/identity")
		if err != nil || len(stubs) != 0 {
			t.Fatalf("bootstrap model scaffold: %v %v", stubs, err)
		}
		for _, side := range []string{"server", "client"} {
			if invocation := target.(spi.Speller).Invoke(family, side, "identity.check"); invocation != (spi.Invocation{}) {
				t.Fatalf("bootstrap model invocation: %+v", invocation)
			}
		}
	}
}

func TestGeneratedGoBootstrapIdentity(t *testing.T) {
	root := repositoryRoot(t)
	fixture(t, root, "go")
	directory := t.TempDir()
	k := kernel.New(golang.New(golang.Config{Module: "example.test/generated"}))
	world := &kernel.World{Families: builtinIdentityWorld()}
	for _, name := range []string{"identity", "consumer"} {
		result, err := k.Render(world, name)
		if err != nil {
			t.Fatal(err)
		}
		for path, data := range result.Files {
			writeFixture(t, directory, path, data)
		}
	}
	fixtureModule(t, directory, root)
	writeFixture(t, directory, "identity_test.go", []byte(goBootstrapIdentity))
	runFixture(t, directory, "go", "test", "-count=1", "-v", ".")
}

func TestGeneratedTypeScriptBootstrapIdentity(t *testing.T) {
	typescriptLanguageFixture(t, builtinIdentityWorld(), tsBootstrapIdentityTypes, tsBootstrapIdentity)
}

const goBootstrapIdentity = `package identity_test
import (
	duplex "github.com/Bitspark/nightseam/duplex/go"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "strings"
 "testing"
 "time"
 identity "example.test/generated/api/go/identity-protocol"
 binding "example.test/generated/api/go/consumer-binding"
 client "example.test/generated/api/go/consumer-client"
 protocol "example.test/generated/api/go/consumer-protocol"

 runtime "github.com/Bitspark/nightseam/runtime/go"
)
type methods struct{}
func (methods) Echo(_ context.Context,p protocol.Input)(int64,error){return p.Value+1,nil}
func TestBootstrapMetadata(t *testing.T){
 value:=identity.Declaration{Path:"consumer"};if err:=identity.ValidateValue("Declaration",value);err!=nil{t.Fatal(err)}
 digest:=sha256.Sum256([]byte(identity.WireDeclaration()));if hex.EncodeToString(digest[:])!=identity.WireDigest(){t.Fatal("canonical identity digest changed")}
 if _,err:=identity.WireSchema().DeclarationDigest();err!=nil{t.Fatal(err)}
}
func TestConsumerConstructors(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
 t.Run("server",func(t *testing.T){
  wire,err:=binding.ToWire(func(protocol.Client)(protocol.Server,error){return protocol.Server{Methods:methods{}},nil},runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
  model,err:=binding.FromWire(ctx,wire,runtime.AdapterContext{});if err!=nil{t.Fatal(err)}
  access,err:=model(protocol.Client{Methods:methods{}});if err!=nil{t.Fatal(err)}
  if n,err:=access.Methods.Echo(ctx,protocol.Input{Value:41});err!=nil||n!=42{t.Fatalf("server roundtrip %d %v",n,err)}
 })
 t.Run("client",func(t *testing.T){
  wire,err:=client.ToWire(func(protocol.Server)(protocol.Client,error){return protocol.Client{Methods:methods{}},nil},runtime.AdapterContext{});if err!=nil{t.Fatal(err)};defer wire.Close(duplex.CodeNormal,"")
  model,err:=client.FromWire(ctx,wire,runtime.AdapterContext{});if err!=nil{t.Fatal(err)}
  access,err:=model(protocol.Server{Methods:methods{}});if err!=nil{t.Fatal(err)}
  if n,err:=access.Methods.Echo(ctx,protocol.Input{Value:41});err!=nil||n!=42{t.Fatalf("client roundtrip %d %v",n,err)}
 })
 for _,role:=range []string{"server","client"}{t.Run(role+" mismatch",func(t *testing.T){
  near,far,err:=runtime.NewWirePair(runtime.Options{});if err!=nil{t.Fatal(err)};defer near.Close(duplex.CodeNormal,"")
  handler,err:=runtime.IdentityHandler(runtime.DeclarationIdentity{Path:"consumer",Digest:strings.Repeat("0",64)});if err!=nil{t.Fatal(err)}
  dispatcher,err:=runtime.NewDispatcher(far);if err!=nil{t.Fatal(err)};defer dispatcher.Close(duplex.CodeNormal,"")
  _,err=runtime.HandleWire(dispatcher,[]string{runtime.IdentityMethod},func(ctx context.Context,raw json.RawMessage)(any,error){return handler(ctx,nil,raw)});if err!=nil{t.Fatal(err)}
  if role=="server"{_,err=binding.FromWire(ctx,near,runtime.AdapterContext{})}else{_,err=client.FromWire(ctx,near,runtime.AdapterContext{})}
  var public *runtime.PublicError;if !errors.As(err,&public)||public.Code!="contract_mismatch"{t.Fatalf("unguarded %s interpretation: %v",role,err)}
 })}
}
`

const tsBootstrapIdentityTypes = `import * as identity from '@example/identity-client';
import * as binding from '@example/consumer-binding';
import * as client from '@example/consumer-client';
const value: identity.Declaration = {path:'consumer'};
identity.validateWire('Declaration',value);
// @ts-expect-error bootstrap vocabulary has no application model constructor
identity.toWire;
// @ts-expect-error bootstrap vocabulary has no model interpretation constructor
identity.fromWire;
// @ts-expect-error bootstrap vocabulary has no deferred model constructor
identity.prepareFromWire;
const server: binding.ServerModel = () => ({methods:{echo({value}){return value+1}},events:{}});
const caller: client.ClientModel = () => ({methods:{echo({value}){return value+1}},events:{}});
binding.toWire(server, {});
client.toWire(caller, {});
`

const tsBootstrapIdentity = `import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {test} from 'node:test';
import * as identity from '@example/identity-client';
import * as binding from '@example/consumer-binding';
import * as client from '@example/consumer-client';
import {DuplexError,IDENTITY_METHOD,identityHandler,registerWire,wirePair,createDispatcher} from '@nightseam/runtime';

test('bootstrap metadata', () => {
 for(const key of ['toWire','fromWire','prepareFromWire'])assert.equal(key in identity,false,key);
 identity.validateWire('Declaration',{path:'consumer'});
 assert.equal(createHash('sha256').update(identity.wireDeclaration).digest('hex'),identity.wireDigest);
});
for (const [role, adapter] of [['server',binding], ['client',client]]) {
 test(role+' construction remains guarded', async () => {
  const implementation={methods:{echo:({value})=>value+1},events:{}};
  const wire=adapter.toWire(()=>implementation,{});
  try {
   const model=await adapter.fromWire(wire,{});
   assert.equal(await model(implementation).methods.echo({value:41}),42);
  } finally {wire.close()}
  const [near,far]=wirePair();
  try {
   registerWire(createDispatcher(far),[IDENTITY_METHOD],{request:identityHandler({path:'consumer',digest:'0'.repeat(64)})});
   await assert.rejects(adapter.fromWire(near,{}),error=>error instanceof DuplexError&&error.code==='contract_mismatch');
  } finally {near.close()}
 });
}
`
