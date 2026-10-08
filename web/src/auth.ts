export type AuthStatus = { mode: "pin" | "passkey"; authenticated: boolean; enrollmentRequired: boolean };

type CreationJSON = {
  publicKey: Omit<PublicKeyCredentialCreationOptions, "challenge" | "user" | "excludeCredentials"> & {
    challenge: string;
    user: Omit<PublicKeyCredentialUserEntity, "id"> & { id: string };
    excludeCredentials?: Array<Omit<PublicKeyCredentialDescriptor, "id"> & { id: string }>;
  };
};

type RequestJSON = {
  publicKey: Omit<PublicKeyCredentialRequestOptions, "challenge" | "allowCredentials"> & {
    challenge: string;
    allowCredentials?: Array<Omit<PublicKeyCredentialDescriptor, "id"> & { id: string }>;
  };
};

export function bufferToBase64URL(value: ArrayBuffer | ArrayBufferView): string {
  const bytes = value instanceof ArrayBuffer
    ? new Uint8Array(value)
    : new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

export function base64URLToBuffer(value: string): ArrayBuffer {
  if (!/^[A-Za-z0-9_-]*$/.test(value)) throw new Error("Relay 返回了无效的 Passkey 数据。");
  const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat((4 - value.length % 4) % 4);
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, char => char.charCodeAt(0));
  return bytes.buffer;
}

async function requestJSON<T>(url: string, body?: unknown): Promise<T> {
  const response = await fetch(url, body === undefined ? { credentials: "same-origin", cache: "no-store" } : {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await response.json().catch(() => ({})) as { error?: string } & T;
  if (!response.ok) throw new Error(payload.error || `认证请求失败（${response.status}）`);
  return payload;
}

export function loadAuthStatus(): Promise<AuthStatus> {
  return requestJSON<AuthStatus>("/api/auth/status");
}

function creationOptions(payload: CreationJSON): PublicKeyCredentialCreationOptions {
  return {
    ...payload.publicKey,
    challenge: base64URLToBuffer(payload.publicKey.challenge),
    user: { ...payload.publicKey.user, id: base64URLToBuffer(payload.publicKey.user.id) },
    excludeCredentials: payload.publicKey.excludeCredentials?.map(item => ({ ...item, id: base64URLToBuffer(item.id) })),
  };
}

function requestOptions(payload: RequestJSON): PublicKeyCredentialRequestOptions {
  return {
    ...payload.publicKey,
    challenge: base64URLToBuffer(payload.publicKey.challenge),
    allowCredentials: payload.publicKey.allowCredentials?.map(item => ({ ...item, id: base64URLToBuffer(item.id) })),
  };
}

function credentialJSON(credential: PublicKeyCredential): unknown {
  const modern = credential as PublicKeyCredential & { toJSON?: () => unknown };
  if (typeof modern.toJSON === "function") return modern.toJSON();
  const common = {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    clientExtensionResults: credential.getClientExtensionResults(),
  };
  const response = credential.response;
  if ("attestationObject" in response) {
    const attestation = response as AuthenticatorAttestationResponse;
    const extended = attestation as AuthenticatorAttestationResponse & {
      getAuthenticatorData?: () => ArrayBuffer;
      getPublicKey?: () => ArrayBuffer | null;
      getPublicKeyAlgorithm?: () => number;
    };
    const publicKey = typeof extended.getPublicKey === "function" ? extended.getPublicKey() : null;
    return {
      ...common,
      response: {
        clientDataJSON: bufferToBase64URL(attestation.clientDataJSON),
        attestationObject: bufferToBase64URL(attestation.attestationObject),
        transports: typeof attestation.getTransports === "function" ? attestation.getTransports() : [],
        ...(typeof extended.getAuthenticatorData === "function" ? { authenticatorData: bufferToBase64URL(extended.getAuthenticatorData()) } : {}),
        ...(publicKey ? { publicKey: bufferToBase64URL(publicKey) } : {}),
        ...(typeof extended.getPublicKeyAlgorithm === "function" ? { publicKeyAlgorithm: extended.getPublicKeyAlgorithm() } : {}),
      },
    };
  }
  const assertion = response as AuthenticatorAssertionResponse;
  return {
    ...common,
    response: {
      clientDataJSON: bufferToBase64URL(assertion.clientDataJSON),
      authenticatorData: bufferToBase64URL(assertion.authenticatorData),
      signature: bufferToBase64URL(assertion.signature),
      userHandle: assertion.userHandle ? bufferToBase64URL(assertion.userHandle) : null,
    },
  };
}

function requirePublicKeyCredential(value: Credential | null): PublicKeyCredential {
  if (!value || value.type !== "public-key") throw new Error("没有创建 Passkey；你可以重新尝试。");
  return value as PublicKeyCredential;
}

export async function registerPasskey(setupToken: string): Promise<void> {
  const options = await requestJSON<CreationJSON>("/api/auth/passkey/register/options", { setupToken });
  const credential = requirePublicKeyCredential(await navigator.credentials.create({ publicKey: creationOptions(options) }));
  await requestJSON("/api/auth/passkey/register/verify", credentialJSON(credential));
}

export async function loginWithPasskey(): Promise<void> {
  const options = await requestJSON<RequestJSON>("/api/auth/passkey/login/options", {});
  const credential = requirePublicKeyCredential(await navigator.credentials.get({ publicKey: requestOptions(options) }));
  await requestJSON("/api/auth/passkey/login/verify", credentialJSON(credential));
}

export async function logoutPasskey(): Promise<void> {
  await requestJSON("/api/auth/logout", {});
}
