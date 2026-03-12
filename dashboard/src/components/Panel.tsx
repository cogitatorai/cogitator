import type { ReactNode } from 'react';

interface PanelProps {
  children: ReactNode;
  className?: string;
}

export default function Panel({ children, className = '' }: PanelProps) {
  return (
    <div className={`hud-panel ${className}`}>
      {children}
    </div>
  );
}
