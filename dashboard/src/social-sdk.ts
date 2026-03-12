// Shared social SDK loaders for dashboard social sign-in.

declare global {
  interface Window {
    AppleID?: {
      auth: {
        init: (config: Record<string, string>) => void;
        signIn: () => Promise<{ authorization: { id_token: string } }>;
      };
    };
    webkit?: {
      messageHandlers?: {
        appleSignIn?: {
          postMessage: (body: unknown) => Promise<{ id_token: string }>;
        };
        notify?: {
          postMessage: (body: unknown) => void;
        };
      };
    };
  }
}

export function loadAppleSDK(): Promise<void> {
  if (document.getElementById('apple-auth')) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.id = 'apple-auth';
    script.src =
      'https://appleid.cdn-apple.com/appleauth/static/jsapi/appleid/1/en_US/appleid.auth.js';
    script.onload = () => resolve();
    script.onerror = () => reject(new Error('Failed to load Apple SDK'));
    document.head.appendChild(script);
  });
}

/** Returns true when running inside the macOS desktop WKWebView wrapper. */
export function isDesktopApp(): boolean {
  return !!window.webkit?.messageHandlers?.appleSignIn;
}

/**
 * Get an Apple ID token, using the native bridge on desktop or the JS SDK on web.
 * Throws on failure or user cancellation.
 */
export async function getAppleIdToken(): Promise<string> {
  if (isDesktopApp()) {
    const result = await window.webkit!.messageHandlers!.appleSignIn!.postMessage({});
    return result.id_token;
  }

  await loadAppleSDK();
  if (!window.AppleID) {
    throw new Error('Apple SDK not available');
  }
  const result = await window.AppleID.auth.signIn();
  return result.authorization.id_token;
}
