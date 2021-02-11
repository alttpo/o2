// this view-model data comes from websocket JSON:
export class ViewModel {
    [k: string]: any;

    status: string;
    snes: SNESViewModel;
    rom: ROMViewModel;
    server: ServerViewModel;
    game: GameViewModel;
}

export interface SNESViewModel {
    drivers?: DriverViewModel[];
    isConnected?: boolean;
}

export interface DriverViewModel {
    name: string;

    displayName: string;
    displayDescription: string;
    displayOrder: number;

    devices: string[];
    selectedDevice: number;

    isConnected: boolean;
}

export interface ROMViewModel {
}

export interface ServerViewModel {
}

export interface GameViewModel {
}
