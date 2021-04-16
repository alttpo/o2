import {GameViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";

type GameProps = {
    ch: CommandHandler;
    game: GameViewModel;
};

function GameView({ch, game}: GameProps) {
    return <div class="grid">
        <h5 class="grid-ca">Game: {game.gameName}</h5>
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<GameView ch={ch} game={vm.game}/>);
