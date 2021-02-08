
// this view-model data comes from websocket JSON:
export interface ViewModel {
    [k: string]: any;

    snes: SNESViewModel;
    rom: ROMViewModel;
    server: ServerViewModel;
    game: GameViewModel;
}

export interface SNESViewModel {

}

export interface ROMViewModel {
}

export interface ServerViewModel {
}

export interface GameViewModel {
}
