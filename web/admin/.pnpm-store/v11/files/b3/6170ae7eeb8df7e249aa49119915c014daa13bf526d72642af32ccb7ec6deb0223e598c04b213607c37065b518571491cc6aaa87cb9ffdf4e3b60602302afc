import { useAppearance } from "../../customizables/AppearanceContext.js";
import { useFormField } from "../../primitives/hooks/useFormField.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { Box, Flex, Text } from "../../customizables/index.js";
import { OTPInputSlot } from "../../elements/CodeControl.js";
import { normalizeOAuthDeviceUserCode } from "./utils.js";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";
import { OTPInput } from "input-otp";

//#region src/components/OAuthDeviceVerification/OAuthDeviceVerificationCodeInput.tsx
const USER_CODE_LENGTH = 8;
const USER_CODE_GROUP_LENGTH = 4;
function CodeGroup({ slots, hasError }) {
	return /* @__PURE__ */ jsx(Flex, {
		sx: (t) => ({ gap: t.space.$1x5 }),
		hasError,
		children: slots.map((slot, index) => /* @__PURE__ */ jsx(OTPInputSlot, {
			elementDescriptor: descriptors.otpCodeFieldInput,
			hasError,
			sx: (t) => ({
				width: t.space.$8,
				height: t.space.$8
			}),
			...slot
		}, index))
	});
}
function OAuthDeviceVerificationCodeInput({ control }) {
	const { autoFocus } = useAppearance().parsedOptions;
	const formField = useFormField();
	const hasError = formField.hasError ?? false;
	return /* @__PURE__ */ jsx(Box, {
		elementDescriptor: descriptors.otpCodeFieldInputContainer,
		sx: { position: "relative" },
		children: /* @__PURE__ */ jsx(OTPInput, {
			id: formField.id,
			name: control.name,
			autoFocus,
			autoComplete: "one-time-code",
			autoCapitalize: "characters",
			"aria-describedby": formField.feedbackMessageId || void 0,
			"aria-invalid": hasError,
			"aria-required": formField.isRequired,
			disabled: formField.isDisabled,
			inputMode: "text",
			maxLength: USER_CODE_LENGTH,
			spellCheck: false,
			textAlign: "center",
			value: control.value,
			pasteTransformer: normalizeOAuthDeviceUserCode,
			onBlur: control.onBlur,
			onFocus: control.onFocus,
			onChange: (value) => {
				control.clearFeedback();
				control.setValue(normalizeOAuthDeviceUserCode(value));
			},
			render: ({ slots }) => /* @__PURE__ */ jsxs(Flex, {
				align: "center",
				elementDescriptor: descriptors.otpCodeFieldInputs,
				gap: 2,
				hasError,
				justify: "center",
				role: "group",
				children: [
					/* @__PURE__ */ jsx(CodeGroup, {
						slots: slots.slice(0, USER_CODE_GROUP_LENGTH),
						hasError
					}),
					/* @__PURE__ */ jsx(Text, {
						"aria-hidden": true,
						"data-testid": "device-code-separator",
						variant: "h2",
						children: "-"
					}),
					/* @__PURE__ */ jsx(CodeGroup, {
						slots: slots.slice(USER_CODE_GROUP_LENGTH),
						hasError
					})
				]
			})
		})
	});
}

//#endregion
export { OAuthDeviceVerificationCodeInput };
//# sourceMappingURL=OAuthDeviceVerificationCodeInput.js.map