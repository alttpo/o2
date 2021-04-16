import {GameViewModel} from "./viewmodel";
import {CommandHandler, TopLevelProps} from "./index";
import {useEffect, useState} from "preact/hooks";
import {setField} from "./util";

type GameProps = {
    ch: CommandHandler;
    game: GameViewModel;
};

function GameView({ch, game}: GameProps) {
    const [syncItems, setsyncItems] = useState(true);
    const [syncDungeonItems, setsyncDungeonItems] = useState(true);
    const [syncProgress, setsyncProgress] = useState(true);
    const [syncHearts, setsyncHearts] = useState(true);

    useEffect(() => {
        setsyncItems(game.syncItems);
        setsyncDungeonItems(game.syncDungeonItems);
        setsyncProgress(game.syncProgress);
        setsyncHearts(game.syncHearts);
    }, [game]);

    const sendGameCommand = ch.command.bind(ch, "game");

    const getTargetChecked = (e: Event) => (e.target as HTMLInputElement).checked;

    return <div class="grid">
        <h5 class="grid-ca">Game: {game.gameName}</h5>

        <label class="grid-c1" for="syncItems">
            <input type="checkbox"
                   id="syncItems"
                   checked={syncItems}
                   onChange={setField.bind(this, sendGameCommand, setsyncItems, "syncItems", getTargetChecked)}/>
            Sync Items
        </label>

        <label class="grid-c1" for="syncDungeonItems">
            <input type="checkbox"
                   id="syncDungeonItems"
                   checked={syncDungeonItems}
                   onChange={setField.bind(this, sendGameCommand, setsyncDungeonItems, "syncDungeonItems", getTargetChecked)}/>
            Sync DungeonItems
        </label>

        <label class="grid-c1" for="syncProgress">
            <input type="checkbox"
                   id="syncProgress"
                   checked={syncProgress}
                   onChange={setField.bind(this, sendGameCommand, setsyncProgress, "syncProgress", getTargetChecked)}/>
            Sync Progress
        </label>

        <label class="grid-c1" for="syncHearts">
            <input type="checkbox"
                   id="syncHearts"
                   checked={syncHearts}
                   onChange={setField.bind(this, sendGameCommand, setsyncHearts, "syncHearts", getTargetChecked)}/>
            Sync Hearts
        </label>
    </div>;
}

export default ({ch, vm}: TopLevelProps) => (<GameView ch={ch} game={vm.game}/>);
