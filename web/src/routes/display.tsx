import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import { EmptyState, Notice, PageHeader } from '@app/design';

import { client } from '../api/client';

const householdKey = ['display', 'household'] as const;

// The wall display. Unpaired, it shows a code for an adult to enter under Displays; once the
// adult has, it collects its session and shows the household (Today arrives in slice 2).
export function DisplayPage() {
  const household = useQuery({
    queryKey: householdKey,
    queryFn: async () => {
      const { data, response } = await client.GET('/household');
      if (response.status === 401) return null;
      if (!data) throw new Error('The household could not be loaded.');
      return data;
    },
    retry: false,
  });

  if (household.isPending) return null;
  if (household.data) {
    return (
      <section>
        <PageHeader title={household.data.name} description="This display is paired." />
        <EmptyState title="Today will show here" description="Chores and routines arrive with the next update." />
      </section>
    );
  }
  return <Pairing />;
}

function Pairing() {
  const queryClient = useQueryClient();
  const [code, setCode] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  // Ask for a code, then ask every few seconds whether an adult has entered it.
  useEffect(() => {
    const run = { stopped: false };
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      const { data, response } = await client.POST('/display/pairings/claim');
      if (run.stopped) return;
      if (response.status === 200 && data) {
        await queryClient.invalidateQueries({ queryKey: householdKey });
        return;
      }
      if (response.status === 404) {
        setAttempt((n) => n + 1); // expired: start over with a new code
        return;
      }
      timer = setTimeout(() => void poll(), 3000);
    };
    void (async () => {
      const { data, response } = await client.POST('/display/pairings');
      if (run.stopped) return;
      if (response.status !== 201 || !data) {
        setProblem('Pairing could not start. Trying again in a moment.');
        timer = setTimeout(() => { setAttempt((n) => n + 1); }, 30000);
        return;
      }
      setProblem(null);
      setCode(data.code);
      timer = setTimeout(() => void poll(), 3000);
    })();
    return () => {
      run.stopped = true;
      clearTimeout(timer);
    };
  }, [attempt, queryClient]);

  return (
    <section>
      <PageHeader title="Pair this display" description="On your phone or computer, open Displays and enter this code." />
      {problem && <Notice tone="attention">{problem}</Notice>}
      {code && (
        <p className="text-center font-mono text-6xl tracking-widest" aria-label={`Pairing code ${code.split('').join(' ')}`}>
          {code}
        </p>
      )}
    </section>
  );
}
