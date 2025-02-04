import crypto from "node:crypto";
import { abs } from "./_utils.ts";

const { publicKey, privateKey } = crypto.generateKeyPairSync("ed25519");
await Deno.mkdir(abs("tmp/paseto"), { recursive: true });
await Deno.writeTextFile(abs("tmp/paseto/private.pem"), privateKey.export({ format: "pem", type: "pkcs8" }).toString());
await Deno.writeTextFile(abs("tmp/paseto/public.pem"), publicKey.export({ format: "pem", type: "spki" }).toString());
