// The runner renders packages beside this testee. Resolve those local
// siblings as a consumer's file: dependencies would, without installing
// or downloading a published package during the conformance run.
export async function resolve(specifier, context, next) {
  if (specifier.startsWith('@example/')) {
    const name = specifier.slice('@example/'.length);
    return {url: new URL(`./api/ts/${name}/src/index.ts`, import.meta.url).href, shortCircuit: true};
  }
  return next(specifier, context);
}
