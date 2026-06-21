import { Modal } from "./ui";
import { LoginPanel } from "./LoginPanel";
import { useI18n } from "../i18n";

export function AuthModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useI18n();
  return (
    <Modal open={open} onClose={onClose} title={t("kino.pub authentication")} wide>
      <LoginPanel onDone={onClose} />
    </Modal>
  );
}
