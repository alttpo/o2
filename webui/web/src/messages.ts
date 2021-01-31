import {ViewModel} from "./viewmodel";

export type O2ViewModelUpdate = {
    c: "vmu";
    d: ViewModel;
}

export type O2IncomingMessage =
    | O2ViewModelUpdate

export class O2OutgoingMessage {
    public c: "";
}
