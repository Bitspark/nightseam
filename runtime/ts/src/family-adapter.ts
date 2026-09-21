import { boundDeclaration, typeDeclaration } from './declaration_identity.ts';
import { validateDrawnType, type AnyFamily, type FamilyBinding } from './validate.ts';
import type { ValueAdapter } from './value-adapter.ts';

/** Select a complete recipe without retaining any operation context or owner. */
export function familyTypeAdapter<F extends AnyFamily, K extends keyof F>(
  family: FamilyBinding<F, K>,
  member: K,
): ValueAdapter<F[K]> {
  const adapter = (family as { readonly types?: { readonly [P in K]?: ValueAdapter<F[P]> } }).types?.[member];
  if (!adapter || typeof adapter.export !== 'function' || typeof adapter.import !== 'function' || !adapter.binding)
    throw new Error('family binding: missing complete interpretation for ' + String(member));
  const expected = { validate: family.validate, type: String(member), slots: family.slots };
  if (member !== 'Envelope' && member !== 'Handle') {
    validateDrawnType(expected, String(member), false);
    validateDrawnType(adapter.binding, String(member), false);
  }
  if (
    boundDeclaration(adapter.binding.validate, adapter.binding.slots) !==
      boundDeclaration(family.validate, family.slots) ||
    typeDeclaration(adapter.binding) !== typeDeclaration(expected)
  )
    throw new Error('family binding: ' + String(member) + ' has a different family interpretation');
  return adapter;
}
