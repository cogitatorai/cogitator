import type { ReactNode, ButtonHTMLAttributes } from 'react';

interface StripedButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  children: ReactNode;
}

export default function StripedButton({ children, className = '', ...props }: StripedButtonProps) {
  return (
    <button
      className={`relative overflow-hidden bg-orange-900/60 border border-orange-600/50 hover:border-orange-500 hover:bg-orange-900/80 text-orange-500 hover:text-orange-400 uppercase tracking-widest text-sm font-medium px-4 py-2.5 transition-colors cursor-pointer disabled:opacity-30 disabled:cursor-not-allowed disabled:hover:border-orange-600/50 disabled:hover:bg-orange-900/60 disabled:hover:text-orange-500 ${className}`}
      {...props}
    >
      {/* Corner accents */}
      <span className="absolute top-0 left-0 w-2 h-2 border-t-2 border-l-2 border-orange-600" />
      <span className="absolute bottom-0 right-0 w-2 h-2 border-b-2 border-r-2 border-orange-600" />
      {/* Striped overlay */}
      <span
        className="absolute inset-0 pointer-events-none opacity-10"
        style={{
          backgroundImage: 'repeating-linear-gradient(45deg, transparent, transparent 5px, currentColor 5px, currentColor 6px)',
        }}
      />
      <span className="relative">{children}</span>
    </button>
  );
}
