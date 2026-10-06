import { redirect } from 'next/navigation';

export default function AgentsRedirectPage() {
  redirect('/settings/models-agents');
}
