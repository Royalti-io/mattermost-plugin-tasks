import type {Store} from 'redux';

import type {GlobalState} from '@mattermost/types/store';

// Gives code outside React components (post menu actions) access to the host store.
let hostStore: Store<GlobalState> | null = null;

export function setStore(store: Store<GlobalState>): void {
    hostStore = store;
}

export function getState(): GlobalState | null {
    return hostStore ? hostStore.getState() : null;
}
