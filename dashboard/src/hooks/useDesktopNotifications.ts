const STORAGE_KEY = 'desktop-notifications';

export function isNotificationsEnabled(): boolean {
  return localStorage.getItem(STORAGE_KEY) !== 'false';
}

/** Enable or disable notifications. When enabling in a browser context,
 *  requests permission and returns false if the user denies it. */
export async function setNotificationsEnabled(enabled: boolean): Promise<boolean> {
  if (enabled && 'Notification' in window) {
    if (Notification.permission === 'default') {
      const result = await Notification.requestPermission();
      if (result !== 'granted') {
        localStorage.setItem(STORAGE_KEY, 'false');
        return false;
      }
    } else if (Notification.permission === 'denied') {
      localStorage.setItem(STORAGE_KEY, 'false');
      return false;
    }
  }
  localStorage.setItem(STORAGE_KEY, String(enabled));
  return enabled;
}

/** On first launch (no stored preference), request browser notification permission.
 *  Skipped in the desktop app where native notifications are used instead. */
export function requestPermissionOnFirstLaunch(): void {
  if ((window as any).webkit?.messageHandlers?.notify) return; // desktop app uses native path
  if (localStorage.getItem(STORAGE_KEY) !== null) return; // user already chose
  if ('Notification' in window && Notification.permission === 'default') {
    Notification.requestPermission();
  }
}

export function sendNotification(
  title: string,
  body: string,
  opts?: { subtitle?: string; page?: string },
): void {
  if (!isNotificationsEnabled()) return;

  const targetPage = opts?.page || 'chat';
  const currentPage = window.location.hash.replace('#', '').split('/')[0] || 'chat';

  // Native macOS bridge (desktop app). Pass the current page so the native
  // side can suppress only when the window is active AND on the target page.
  const wk = (window as any).webkit?.messageHandlers?.notify;
  if (wk) {
    wk.postMessage({
      title,
      body,
      subtitle: opts?.subtitle || '',
      page: targetPage,
      currentPage,
    });
    return;
  }

  // Browser fallback: skip if the tab is focused and on the target page.
  if (document.hasFocus() && currentPage === targetPage) return;
  if ('Notification' in window && Notification.permission === 'granted') {
    new Notification(`Cogitator: ${title}`, { body });
  }
}
