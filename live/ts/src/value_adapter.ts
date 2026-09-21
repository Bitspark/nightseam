import { DuplexError, type ValueEnvironment } from '@nightseam/runtime';
import { LiveOwner, type LiveScope } from './index.ts';

/** Binds effects to a scope; owners are selected separately for each operation. */
export function valueEnvironment(scope: LiveScope): ValueEnvironment {
  const select = (context: unknown): LiveOwner => {
    if (!scope) throw new DuplexError('scope_closed', 'Live conversion requires a scope.');
    return context instanceof LiveOwner && context.scope === scope ? context : scope.owner();
  };
  return {
    select,
    child(context) {
      return select(context).child();
    },
    export(context, build) {
      return select(context).exportValue(build);
    },
    import(context, build) {
      return select(context).importValue(build);
    },
    publish(context, build, publish) {
      return select(context).publishValue(build, publish);
    },
  };
}
