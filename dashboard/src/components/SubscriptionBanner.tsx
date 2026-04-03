import { useState, useEffect } from 'react';
import { fetchJSON } from '../api';

interface SubscriptionStatus {
  status: 'active' | 'past_due' | 'grace_period' | 'expired';
  grace_days_remaining?: number;
}

export default function SubscriptionBanner() {
  const [sub, setSub] = useState<SubscriptionStatus | null>(null);

  useEffect(() => {
    fetchJSON<SubscriptionStatus>('/api/subscription-status')
      .then(setSub)
      .catch(() => {});
  }, []);

  if (!sub || sub.status === 'active') return null;

  const portalUrl = '/api/billing/portal';

  if (sub.status === 'expired') {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm">
        <div className="border border-red-500/40 bg-zinc-900 p-8 max-w-md text-center">
          <p className="text-[12px] uppercase tracking-widest font-semibold text-red-500 mb-3">
            Subscription Expired
          </p>
          <p className="text-sm text-zinc-400 mb-6">
            Resubscribe to restore access to your Cogitator instance.
          </p>
          <a
            href={portalUrl}
            className="inline-block px-6 py-2.5 text-sm font-medium uppercase tracking-widest bg-orange-600 text-white hover:bg-orange-500 transition-colors"
          >
            Resubscribe
          </a>
        </div>
      </div>
    );
  }

  const isGrace = sub.status === 'grace_period';
  const borderColor = isGrace ? 'border-red-500/40' : 'border-amber-500/40';
  const bgColor = isGrace ? 'bg-red-950/30' : 'bg-amber-950/30';
  const textColor = isGrace ? 'text-red-500' : 'text-amber-500';

  return (
    <div className={`w-full border ${borderColor} ${bgColor} p-4 flex items-center justify-between`}>
      <p className={`text-[12px] uppercase tracking-widest font-medium ${textColor}`}>
        {isGrace
          ? `Your subscription has ended. You have ${sub.grace_days_remaining ?? 0} day${(sub.grace_days_remaining ?? 0) === 1 ? '' : 's'} to resubscribe.`
          : 'Payment failed. Please update your payment method.'}
      </p>
      <a
        href={portalUrl}
        className="shrink-0 ml-4 px-4 py-2 text-sm font-medium uppercase tracking-widest bg-orange-600 text-white hover:bg-orange-500 transition-colors"
      >
        {isGrace ? 'Resubscribe' : 'Update Payment'}
      </a>
    </div>
  );
}
