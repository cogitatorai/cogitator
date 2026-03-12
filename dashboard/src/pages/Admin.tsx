import PageHeader from '../components/PageHeader';
import ModelsSection from '../components/ModelsSection';

export default function Admin() {
  return (
    <div className="space-y-6">
      <PageHeader title="Admin" subtitle="System configuration for all users" />
      <ModelsSection />
    </div>
  );
}
