import {GameViewComponent, GameViewProps} from "./viewmodel";
import {TopLevelProps} from "./index";

// import specific game views:
import {GameViewALTTP} from "./games/alttp";

const gameViews: { [gameName: string]: GameViewComponent } = {
    "ALTTP": GameViewALTTP
};

function GameView({ch, vm}: GameViewProps) {
    if (!vm.game.gameName) {
        return <div/>;
    }

    // route to specific game view based on `gameName`:
    const DynamicGameView = gameViews[vm.game.gameName];
    return <DynamicGameView ch={ch} vm={vm}/>;
}

export default ({ch, vm}: TopLevelProps) => <GameView ch={ch} vm={vm}/>;
