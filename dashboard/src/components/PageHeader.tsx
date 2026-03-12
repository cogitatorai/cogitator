interface PageHeaderProps {
  title: string;
  subtitle?: string;
}

export default function PageHeader({ title, subtitle }: PageHeaderProps) {
  return (
    <div className="mb-6">
      <h2 className="text-4xl font-semibold uppercase tracking-[0.1em] text-zinc-100">
        {title}
      </h2>
      <div className="h-[2px] w-16 bg-orange-600 mt-2" />
      {subtitle && (
        <p className="text-[13px] text-zinc-500 mt-3 uppercase tracking-[0.2em]">{subtitle}</p>
      )}
    </div>
  );
}
