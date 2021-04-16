import {GameViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";

type GameProps = {
    ch: CommandHandler;
    game: GameViewModel;
};

function GameView({ch, game}: GameProps) {
    return <div class="card three-grid">
        <h5>{game.gameName}</h5>
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<GameView ch={ch} game={vm.game}/>);
