// The conformance recipe requires only the JDK and Node already used by the suite.
// Gradle consumers can use the equivalent multi-project build in this directory.
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync, copyFileSync } from 'node:fs';
import { dirname, join, resolve, relative, delimiter } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';

const self = dirname(fileURLToPath(import.meta.url));
const root = resolve(self, '../..');
const out = resolve(process.argv[2] ?? join(self, 'build'));
const classes = join(out, 'classes');
if (relative(out, classes) !== 'classes') throw new Error('class output escaped the requested build directory');
rmSync(classes, { recursive: true, force: true });
mkdirSync(classes, { recursive: true });
function run(command, args, cwd = root) {
  const result = spawnSync(command, args, { cwd, stdio: 'inherit', windowsHide: true });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} exited ${result.status}`);
}
function files(path) {
  if (!existsSync(path)) return [];
  return readdirSync(path, { withFileTypes: true }).flatMap(entry =>
    entry.isDirectory() ? files(join(path, entry.name)) : entry.name.endsWith('.java') ? [join(path, entry.name)] : []);
}
// Bitwire is an upstream artifact, never included in either Nightseam jar.
const bitwire = join(out, 'bitwire-0.2.0.jar');
if (!existsSync(bitwire)) {
  const response = await fetch('https://repo.maven.apache.org/maven2/dev/bitspark/bitwire/0.2.0/bitwire-0.2.0.jar');
  if (!response.ok) throw new Error(`Bitwire download failed: ${response.status}`);
  writeFileSync(bitwire, Buffer.from(await response.arrayBuffer()));
}
// The standalone testee launches from one class directory.
run('jar', ['--extract', '--file', bitwire], classes);
const components = ['duplex/java', 'runtime/java', 'conformance/java'];
const source = components.flatMap(part => files(join(root, part, 'src/main')));
const args = join(out, 'sources.args');
writeFileSync(args, source.map(path => JSON.stringify(path.replaceAll('\\', '/'))).join('\n'));
run('javac', ['--release', '21', '-encoding', 'UTF-8', '-cp', classes, '-d', classes, `@${args}`]);
const notices = join(out, 'notices');
mkdirSync(notices, { recursive: true });
copyFileSync(join(root, 'LICENSE'), join(notices, 'LICENSE'));
copyFileSync(join(root, 'NOTICE'), join(notices, 'NOTICE'));
const jars = [bitwire];
for (const component of ['duplex', 'runtime']) {
  const artifact = join(out, `nightseam-${component}.jar`);
  run('jar', ['--create', '--file', artifact, '-C', classes, `io/nightseam/${component}`, '-C', notices, 'LICENSE', '-C', notices, 'NOTICE']);
  jars.push(artifact);
}
if (process.argv.includes('--test')) {
  const testClasses = join(out, 'test-classes');
  mkdirSync(testClasses, { recursive: true });
  const tests = components.flatMap(part => files(join(root, part, 'src/test')));
  const testArgs = join(out, 'tests.args');
  writeFileSync(testArgs, tests.map(path => JSON.stringify(path.replaceAll('\\', '/'))).join('\n'));
  run('javac', ['--release', '21', '-encoding', 'UTF-8', '-cp', classes, '-d', testClasses, `@${testArgs}`]);
  for (const name of ['io.nightseam.duplex.SeamTest', 'io.nightseam.runtime.SchemaTest', 'io.nightseam.runtime.PeerTest', 'io.nightseam.runtime.PeerWireTest', 'io.nightseam.runtime.PeerLifecycleTest',
    'io.nightseam.runtime.WirePairTest', 'io.nightseam.conformance.RecordedWireTest']) {
    console.log(`Java native suite: ${name}`);
    run('java', ['-ea', '-cp', [classes, testClasses].join(delimiter), name, root]);
  }
  // An isolated consumer has no source tree, class directory or workspace dependency.
  const consumer = mkdtempSync(join(tmpdir(), 'nightseam-java-consumer-'));
  try {
    for (const jar of jars) copyFileSync(jar, join(consumer, jar.split(/[\\/]/).at(-1)));
    const classpath = ['bitwire-0.2.0.jar', 'nightseam-duplex.jar', 'nightseam-runtime.jar'].join(delimiter);
    writeFileSync(join(consumer, 'Consumer.java'), `
import io.nightseam.duplex.Pipe;
import io.nightseam.runtime.*;
import java.util.concurrent.TimeUnit;
public class Consumer {
  public static void main(String[] args) throws Exception {
    var connections = Pipe.pair(1048576, 8);
    try (var server = new Peer(connections[0], "server", PeerOptions.defaults());
         var client = new Peer(connections[1], "client", PeerOptions.defaults())) {
      server.handle("echo", (context, params) -> params);
      var result = client.call("echo", Json.parse("{\\"present\\":null}")).result().get(3, TimeUnit.SECONDS);
      var value = Json.object(result);
      if (!value.containsKey("present") || value.get("present") != null || value.containsKey("absent")) throw new AssertionError();
    }
  }
}
`);
    run('javac', ['--release', '21', '-cp', classpath, 'Consumer.java'], consumer);
    run('java', ['-ea', '-cp', `.${delimiter}${classpath}`, 'Consumer'], consumer);
  } finally { rmSync(consumer, { recursive: true, force: true }); }
  console.log('Java native suites and outside-workspace artifact smoke passed');
}
