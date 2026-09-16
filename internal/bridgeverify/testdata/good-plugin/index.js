export const name = "sample-good";
export const inject = ["tools"];
export function apply(ctx, config) {
  ctx.effect(() => ctx.on("tools/pre-execute", async (exec, next) => next()), "sample: pre-execute");
  ctx.tools.register({ name: "sample-tool", apply: () => {} });
}
