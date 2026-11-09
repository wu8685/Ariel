import { useRef } from "react";

export type ImeEnterDecision = "submit" | "composition" | "post-composition" | "other";

type KeyboardLike = {
  key?: string;
  shiftKey?: boolean;
  isComposing?: boolean;
  keyCode?: number;
  nativeEvent?: { isComposing?: boolean; keyCode?: number };
};

type ImeEnterGuardOptions = {
  now?: () => number;
  postCompositionMs?: number;
};

export type ImeEnterGuard = {
  compositionStart: () => void;
  compositionEnd: () => void;
  blur: () => void;
  classify: (event: KeyboardLike) => ImeEnterDecision;
  blocksFormSubmit: () => boolean;
  allowExplicitFormSubmit: () => void;
};

const safariTailGuardMs = 50;

export function createImeEnterGuard(options: ImeEnterGuardOptions = {}): ImeEnterGuard {
  const now = options.now || (() => performance.now());
  const postCompositionMs = options.postCompositionMs ?? safariTailGuardMs;
  let composing = false;
  let ignoreEnterUntil: number | undefined;
  let blockFormSubmitUntil: number | undefined;
  let explicitFormSubmit = false;

  return {
    compositionStart() { composing = true; },
    compositionEnd() {
      composing = false;
      ignoreEnterUntil = now() + postCompositionMs;
    },
    blur() { composing = false; },
    classify(event) {
      if (event.key !== "Enter" || event.shiftKey) return "other";
      const native = event.nativeEvent || {};
      if (event.isComposing === true || native.isComposing === true || event.keyCode === 229 || native.keyCode === 229 || composing) {
        blockFormSubmitUntil = now() + postCompositionMs;
        return "composition";
      }
      if (ignoreEnterUntil !== undefined && now() <= ignoreEnterUntil) {
        blockFormSubmitUntil = ignoreEnterUntil;
        return "post-composition";
      }
      return "submit";
    },
    blocksFormSubmit() {
      if (explicitFormSubmit) {
        explicitFormSubmit = false;
        return false;
      }
      const timestamp = now();
      return composing || (ignoreEnterUntil !== undefined && timestamp <= ignoreEnterUntil) || (blockFormSubmitUntil !== undefined && timestamp <= blockFormSubmitUntil);
    },
    allowExplicitFormSubmit() { explicitFormSubmit = true; },
  };
}

export function useImeEnterGuard(options: ImeEnterGuardOptions = {}): ImeEnterGuard {
  const guard = useRef<ImeEnterGuard | undefined>(undefined);
  if (!guard.current) guard.current = createImeEnterGuard(options);
  return guard.current;
}
