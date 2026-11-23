// this view-model data comes from websocket JSON:
import {CommandHandler} from "./index";
import {JSX} from "preact";

export interface ViewModel {
    [k: string]: any;

    status?: string;
    snes?: SNESViewModel;
    rom?: ROMViewModel;
    server?: ServerViewModel;
    game?: GameViewModel;
}

export interface TimestampedNotification {
    t: string; // timestamp
    m: string; // message
}

export interface SNESViewModel {
    drivers: DriverViewModel[];
    isConnected: boolean;
}

export interface DriverViewModel {
    name: string;

    displayName: string;
    displayDescription: string;
    displayOrder: number;

    devices: DeviceViewModel[];
    selectedDevice: string;

    isConnected: boolean;
}

export interface DeviceViewModel {
    id: string;
    displayName: string;

    [k: string]: any;
}

export interface ROMViewModel {
    isLoaded: boolean;

    name: string;
    title: string;
    region: string;
    version: string;
    // the name of the O2 patched ROM, including the O2 patch version; the Boot command uses it if filename is blank:
    patchedFilename?: string;
    // the O2 patch version the ROM was patched with:
    patchVersion?: string;

    folder: string;
    filename: string;
}

export interface ServerViewModel {
    isConnected: boolean;

    hostName: string;
    groupName: string;
    playerName: string;
    team: number;
}

export interface GameViewModel {
    isCreated: boolean;
    gameName: string;
    // set once the SNES is confirmed to be running the ROM that O2 patched:
    romVerified?: boolean;
    // why the ROM is not verified, or that it is:
    romStatus?: string;
}

export interface GameALTTPViewModel extends GameViewModel {
    playerColor: number;
    syncItems: boolean;
    syncDungeonItems: boolean;
    syncProgress: boolean;
    syncHearts: boolean;
    syncSmallKeys: boolean;
    syncUnderworld: boolean;
    syncOverworld: boolean;
    syncUnderworldLocal: boolean;
    syncOverworldLocal: boolean;
    syncTorches: boolean;
    syncEnemiesKilled: boolean;
    killDeadEnemies: boolean;
    syncSfx: boolean;
    disableBgMusic: boolean;
    syncChests: boolean;
    syncTunicColor: boolean;
    syncSprites: boolean;
    // switched on the /debug page: the game view can change what the player has (see games/alttp/debugsections.tsx):
    debugMode?: boolean;
}

export type GameViewProps = {
    ch: CommandHandler;
    vm: ViewModel;
};

export type GameViewComponent = ({ch, vm}: GameViewProps) => JSX.Element;
