/**
 * Both installation smokes hold the same data/RPC/live exchange. The final
 * result comes from the server calling the client's notice inside the stop
 * function returned earlier, after the request carrying both has ended.
 * Match complete lines so an incorrect value with a valid prefix fails too.
 */
export function holdProbeExchange(output) {
  const lines = new Set(output.split(/\r?\n/));
  for (const line of ["echo    -> olleh", "changed -> hello", "notice  -> demo", "stopped -> demo done"]) {
    if (!lines.has(line)) throw new Error(`the example printed no ${JSON.stringify(line)}:\n${output}`);
  }
}
