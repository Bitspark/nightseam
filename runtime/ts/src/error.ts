/** Public application errors may cross the wire; other handler errors are hidden. */
export class DuplexError extends Error {
  /** The error's code as it travels on the wire: the profile's own, or a family's public error by name. */
  readonly code: string;
  /** What a public error carries beside its message, validated as the family declares it. */
  readonly data?: unknown;

  constructor(code: string, message: string, data?: unknown) {
    super(message);
    this.name = 'DuplexError';
    this.code = code;
    this.data = data;
  }
}
