import { localizationKeys } from "../../localization/localizationKeys.js";
import { useLocalizations } from "../../localization/makeLocalizable.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Badge, Button, Col, Flex, Text } from "../../customizables/index.js";
import { Alert } from "../../elements/Alert.js";
import { handleError } from "../../utils/errorHandler.js";
import { useState } from "react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/ConfigureDirectorySync/SyncNowRow.tsx
const STATUS_COLOR_SCHEME = {
	succeeded: "success",
	running: "primary",
	failed: "danger",
	cancelled: "warning"
};
/**
* Starts a sync and reports how the last one went.
*
* A pull directory is read on a schedule, so without this the setup flow shows
* an empty user list for minutes with no way to tell a slow sync from a broken
* one.
*/
const SyncNowRow = ({ status, onSync, onSynced }) => {
	const { t } = useLocalizations();
	const [isSyncing, setIsSyncing] = useState(false);
	const [error, setError] = useState(void 0);
	const run = async () => {
		if (isSyncing) return;
		setError(void 0);
		setIsSyncing(true);
		try {
			await onSync();
			onSynced();
		} catch (err) {
			try {
				handleError(err, [], (message) => setError(typeof message === "string" ? message : void 0));
			} catch {
				setError(t(localizationKeys("configureDirectorySync.testStep.error__syncFailed")));
			}
		} finally {
			setIsSyncing(false);
		}
	};
	const lastStatus = status?.lastSyncStatus ?? null;
	return /* @__PURE__ */ jsxs(Col, {
		elementDescriptor: descriptors.configureDirectorySyncSyncRow,
		sx: (t) => ({
			gap: t.space.$3,
			padding: t.space.$4,
			borderRadius: t.radii.$md,
			borderWidth: t.borderWidths.$normal,
			borderStyle: t.borderStyles.$solid,
			borderColor: t.colors.$borderAlpha150
		}),
		children: [
			/* @__PURE__ */ jsxs(Flex, {
				align: "center",
				justify: "between",
				sx: (t) => ({ gap: t.space.$3 }),
				children: [/* @__PURE__ */ jsxs(Col, {
					sx: (t) => ({ gap: t.space.$0x5 }),
					children: [/* @__PURE__ */ jsxs(Flex, {
						align: "center",
						sx: (t) => ({ gap: t.space.$2 }),
						children: [/* @__PURE__ */ jsx(Text, {
							as: "span",
							localizationKey: localizationKeys("configureDirectorySync.testStep.syncRow.title"),
							sx: (t) => ({
								fontSize: t.fontSizes.$sm,
								fontWeight: t.fontWeights.$medium
							})
						}), lastStatus && /* @__PURE__ */ jsx(Badge, {
							elementDescriptor: descriptors.configureDirectorySyncStatusBadge,
							elementId: descriptors.configureDirectorySyncStatusBadge.setId(lastStatus),
							colorScheme: STATUS_COLOR_SCHEME[lastStatus],
							localizationKey: localizationKeys(`configureDirectorySync.testStep.syncStatus__${lastStatus}`)
						})]
					}), /* @__PURE__ */ jsx(Text, {
						elementDescriptor: descriptors.configureDirectorySyncLastSyncedAt,
						as: "span",
						colorScheme: "secondary",
						sx: (t) => ({ fontSize: t.fontSizes.$sm }),
						localizationKey: status?.lastSyncedAt ? void 0 : localizationKeys("configureDirectorySync.testStep.syncRow.neverSynced"),
						children: status?.lastSyncedAt ? status.lastSyncedAt.toLocaleString() : void 0
					})]
				}), /* @__PURE__ */ jsx(Button, {
					elementDescriptor: descriptors.configureDirectorySyncSyncNowButton,
					variant: "outline",
					size: "sm",
					isLoading: isSyncing,
					onClick: () => void run(),
					localizationKey: localizationKeys("configureDirectorySync.testStep.actionLabel__syncNow"),
					sx: { flexShrink: 0 }
				})]
			}),
			status?.lastSyncError && /* @__PURE__ */ jsx(Alert, {
				variant: "danger",
				title: localizationKeys("configureDirectorySync.testStep.error__lastSyncFailed"),
				subtitle: status.lastSyncError
			}),
			error && /* @__PURE__ */ jsx(Alert, {
				variant: "danger",
				title: error
			})
		]
	});
};

//#endregion
export { SyncNowRow };
//# sourceMappingURL=SyncNowRow.js.map