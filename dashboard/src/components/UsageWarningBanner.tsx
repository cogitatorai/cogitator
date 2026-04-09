import { useEffect, useState } from 'react';
import { fetchJSON } from '../api';

interface UsageWarning {
  level: string;
  usage_pct: number;
  period_end: string;
  upgrade_url: string;
}

export default function UsageWarningBanner() {
  const [warning, setWarning] = useState<UsageWarning | null>(null);
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    fetchJSON<UsageWarning>('/api/usage-warning')
      .then((data) => {
        if (data.level) {
          setWarning(data);
        }
      })
      .catch(() => {}); // Silently fail for non-SaaS
  }, []);

  if (!warning || !warning.level || dismissed) return null;

  const isExceeded = warning.usage_pct >= 100;
  const isCritical = warning.level === 'critical' || isExceeded;

  const bgColor = isCritical ? 'bg-red-50 border-red-200' : 'bg-yellow-50 border-yellow-200';
  const textColor = isCritical ? 'text-red-800' : 'text-yellow-800';

  let message: string;
  if (isExceeded) {
    message = 'Token limit reached. Upgrade your plan to continue.';
  } else if (isCritical) {
    message = `You've used ${Math.round(warning.usage_pct)}% of your token allowance. Upgrade to keep using Cogitator.`;
  } else {
    message = `You've used ${Math.round(warning.usage_pct)}% of your token allowance this period.`;
  }

  const periodEnd = warning.period_end
    ? new Date(warning.period_end).toLocaleDateString(undefined, { month: 'long', day: 'numeric' })
    : '';

  return (
    <div className={`border rounded-lg px-4 py-3 mb-4 flex items-center justify-between ${bgColor}`}>
      <div className={textColor}>
        <span className="font-medium">{message}</span>
        {periodEnd && <span className="ml-2 text-sm opacity-75">Resets {periodEnd}.</span>}
      </div>
      <div className="flex items-center gap-2">
        {warning.upgrade_url && (
          <a
            href={warning.upgrade_url}
            className="text-sm font-medium underline"
          >
            Upgrade
          </a>
        )}
        {!isExceeded && (
          <button
            onClick={() => setDismissed(true)}
            className="text-sm opacity-50 hover:opacity-100"
          >
            Dismiss
          </button>
        )}
      </div>
    </div>
  );
}
