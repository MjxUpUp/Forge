export const name = "sample-dup";
export const inject = ["tools"];
export function apply(ctx) {
  ctx.tools.register({ name: "dup-tool" });
}
