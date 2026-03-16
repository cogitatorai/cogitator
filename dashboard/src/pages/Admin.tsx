import PageHeader from '../components/PageHeader';
import ModelsSection from '../components/ModelsSection';

export default function Admin() {
  return (
    <div className="space-y-6">
      <PageHeader title="Models" subtitle="LLM provider configuration" />
      <ModelsSection />
    </div>
  );
}
