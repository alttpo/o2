import {GameALTTPViewModel, GameViewProps} from "../viewmodel";
import {useEffect, useState} from "preact/hooks";
import {setField} from "../util";

export function GameViewALTTP({ch, vm}: GameViewProps) {
    const game = vm.game as GameALTTPViewModel;
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

        <label class="grid-c1" for="syncDungeonItems"
               title="Big Keys, Compasses, Maps">
            <input type="checkbox"
                   id="syncDungeonItems"
                   checked={syncDungeonItems}
                   onChange={setField.bind(this, sendGameCommand, setsyncDungeonItems, "syncDungeonItems", getTargetChecked)}/>
            Sync Dungeon Items
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
