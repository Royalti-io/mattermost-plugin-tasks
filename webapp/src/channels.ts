import {useSelector} from 'react-redux';

import type {GlobalState} from '@mattermost/types/store';

export type ChannelChoice = {id: string; label: string};

// Channels the user can put a task in: the public and private channels they belong to,
// plus `extraId` (usually the open channel) even when it is a direct or group message.
export function useChannelChoices(extraId?: string): ChannelChoice[] {
    const channels = useSelector((s: GlobalState) => s.entities.channels.channels);
    const myMembers = useSelector((s: GlobalState) => s.entities.channels.myMembers);

    const choices = Object.values(channels).
        filter((c) => myMembers[c.id] && (c.type === 'O' || c.type === 'P') && c.delete_at === 0).
        sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name)).
        map((c) => ({id: c.id, label: '~' + (c.display_name || c.name)}));

    const extra = extraId ? channels[extraId] : undefined;
    if (extra && myMembers[extra.id] && extra.delete_at === 0 && !choices.some((c) => c.id === extra.id)) {
        let label = 'This conversation';
        if (extra.type === 'D') {
            label = 'This direct message';
        } else if (extra.type === 'G') {
            label = 'This group message';
        }
        choices.unshift({id: extra.id, label});
    }
    return choices;
}
