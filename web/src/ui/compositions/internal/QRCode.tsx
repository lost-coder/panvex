// U8: re-export QRCodeSVG as a default export so React.lazy can use it
// without an adapter. Sits in internal/ so it's not part of the public
// package surface — this file exists only to isolate qrcode.react
// into its own chunk.
import { QRCodeSVG } from "qrcode.react";

export default QRCodeSVG;
