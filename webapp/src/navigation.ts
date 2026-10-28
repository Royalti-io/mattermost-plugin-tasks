export const BOARD_PATH = '/tasks-board';

type WebappUtils = {browserHistory?: {push: (path: string) => void}};

// Opens the board inside the single-page app when Mattermost exposes its router,
// and falls back to a full page load.
export function openBoard(): void {
    const history = (window as unknown as {WebappUtils?: WebappUtils}).WebappUtils?.browserHistory;
    if (history) {
        history.push(BOARD_PATH);
    } else {
        window.location.assign(BOARD_PATH);
    }
}
