// The runner renders packages beside this testee. Resolve those local
// siblings as a consumer's file: dependencies would, without installing
// or downloading a published package during the conformance run.
import { pathToFileURL } from 'node:url';
const testeePackage = pathToFileURL('{checkout}/conformance/ts/package.json').href;

export async function resolve(specifier, context, next) {
  if (specifier === 'ws') return next(specifier, { ...context, parentURL: testeePackage });
  if (specifier.startsWith('@example/')) {
    const name = specifier.slice('@example/'.length);
    if (name.endsWith('/types')) return {url: new URL(`./api/ts/${name.slice(0, -6)}/src/types.ts`, import.meta.url).href, shortCircuit: true};
    return {url: new URL(`./api/ts/${name}/src/index.ts`, import.meta.url).href, shortCircuit: true};
  }
  return next(specifier, context);
}
