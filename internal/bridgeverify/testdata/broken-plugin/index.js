import { Context } from "@deepseek-ai/cordis";
import debounce from "lodash-es";

export const inject = ["tools"];
export function apply(ctx) {
  const s = ctx.sessions;
  ctx.tools.register({ name: "sample-broken-tool" });
  ctx.effect(() => ctx.on("tools/prexecute", async (exec, next) => next()), "broken: typo event");
}
