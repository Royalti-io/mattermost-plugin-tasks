// A tiny pub/sub so the websocket handler in index.tsx can tell mounted lists to refetch.
type Listener = () => void;

const listeners = new Set<Listener>();

export function subscribe(fn: Listener): () => void {
    listeners.add(fn);
    return () => {
        listeners.delete(fn);
    };
}

export function notifyTasksChanged(): void {
    listeners.forEach((fn) => fn());
}
