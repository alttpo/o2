import {GameViewComponent, GameViewProps} from "./viewmodel";
import {TopLevelProps} from "./index";

// import specific game views:
import {GameViewALTTP} from "./games/alttp";

const gameViews: { [gameName: string]: GameViewComponent } = {
    "ALTTP": GameViewALTTP
};

function GameView({ch, vm}: GameViewProps) {
    if (!vm.snes.isConnected) {
        return <h2>Not connected to a SNES device!</h2>;
    }
    if (!vm.server.isConnected) {
        return <h2>Not connected to the game server!</h2>;
    }
    if (!vm.game.isCreated) {
        return <h2>No game ROM selected!</h2>;
    }

    // route to specific game view based on `gameName`:
    const DynamicGameView = gameViews[vm.game.gameName];
    return <DynamicGameView ch={ch} vm={vm}/>;
}

export default ({ch, vm}: TopLevelProps) => <GameView ch={ch} vm={vm}/>;
