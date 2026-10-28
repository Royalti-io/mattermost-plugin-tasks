
import {api} from 'api';
import type {ListParams} from 'api';
import {useCallback, useEffect, useRef, useState} from 'react';
import {subscribe} from 'taskEvents';
import type {Task} from 'taskTypes';

export type TasksState = {
    tasks: Task[];
    loading: boolean;
    error: string | null;
};

// Loads tasks for a query and reloads when the query changes or the server pushes a change.
export function useTasks(params: ListParams, enabled = true) {
    const [state, setState] = useState<TasksState>({tasks: [], loading: true, error: null});
    const seq = useRef(0);
    const key = JSON.stringify(params);

    const load = useCallback(async () => {
        const mine = ++seq.current;
        try {
            const tasks = await api.listTasks(JSON.parse(key));
            if (mine === seq.current) {
                setState({tasks, loading: false, error: null});
            }
        } catch (err) {
            if (mine === seq.current) {
                setState((s) => ({...s, loading: false, error: err instanceof Error ? err.message : 'Failed to load tasks'}));
            }
        }
    }, [key]);

    useEffect(() => {
        if (!enabled) {
            return undefined;
        }
        setState((s) => ({...s, loading: true}));
        load();
        return undefined;
    }, [load, enabled]);

    useEffect(() => {
        if (!enabled) {
            return undefined;
        }
        let timer: ReturnType<typeof setTimeout> | undefined;
        const unsubscribe = subscribe(() => {
            if (timer) {
                clearTimeout(timer);
            }
            timer = setTimeout(load, 250);
        });
        return () => {
            unsubscribe();
            if (timer) {
                clearTimeout(timer);
            }
        };
    }, [load, enabled]);

    const replace = useCallback((task: Task) => {
        setState((s) => ({...s, tasks: s.tasks.map((t) => (t.id === task.id ? task : t))}));
    }, []);

    return {...state, reload: load, replace};
}
