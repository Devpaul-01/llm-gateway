import { useParams } from 'react-router-dom';
import { PageHeader } from '../../components/layout/PageHeader';

export function ProjectDetailPage() {
  const { projectId } = useParams();
  return <PageHeader title="Project" description={projectId} />;
}
