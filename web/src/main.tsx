import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { takePairingCredential } from "./pairing";
import { configureIOSInputViewport } from "./viewport";
import "./style.css";

configureIOSInputViewport(document, navigator);
const initialPairingCredential = takePairingCredential(window.location, window.history);
createRoot(document.getElementById("root")!).render(<React.StrictMode><App initialPairingCredential={initialPairingCredential} /></React.StrictMode>);
