import { localizationKeys } from "../../localization/localizationKeys.js";
import { descriptors } from "../../customizables/elementDescriptors.js";
import { withCardStateProvider } from "../../elements/contexts/index.js";
import { Button, Col } from "../../customizables/index.js";
import { useFieldOTP } from "../../elements/CodeControl.js";
import { Form } from "../../elements/Form.js";
import { FormButtonContainer } from "../../elements/FormButtons.js";
import { FormContainer } from "../../elements/FormContainer.js";
import React from "react";
import { useUser } from "@clerk/shared/react";
import { jsx, jsxs } from "@emotion/react/jsx-runtime";

//#region src/components/UserProfile/VerifyTOTP.tsx
const VerifyTOTP = withCardStateProvider((props) => {
	const { onSuccess, onReset, onBack, verifiedTotpRef } = props;
	const { user } = useUser();
	const otp = useFieldOTP({
		onCodeEntryFinished: (code, resolve, reject) => {
			user?.verifyTOTP({ code }).then((totp) => resolve(totp)).catch(reject);
		},
		onResolve: (a) => {
			verifiedTotpRef.current = a;
			onSuccess();
		}
	});
	return /* @__PURE__ */ jsxs(FormContainer, {
		headerTitle: localizationKeys("userProfile.mfaTOTPPage.title"),
		children: [/* @__PURE__ */ jsx(Col, { children: /* @__PURE__ */ jsx(Form.OTPInput, {
			...otp,
			label: localizationKeys("userProfile.mfaTOTPPage.verifyTitle"),
			description: localizationKeys("userProfile.mfaTOTPPage.verifySubtitle")
		}) }), /* @__PURE__ */ jsxs(FormButtonContainer, {
			sx: { marginTop: 0 },
			children: [/* @__PURE__ */ jsx(Button, {
				onClick: onReset,
				variant: "ghost",
				isDisabled: otp.isLoading,
				localizationKey: localizationKeys("userProfile.formButtonReset"),
				elementDescriptor: descriptors.formButtonReset
			}), /* @__PURE__ */ jsx(Button, {
				onClick: onBack,
				variant: "ghost",
				isDisabled: otp.isLoading,
				localizationKey: localizationKeys("backButton"),
				elementDescriptor: descriptors.backLink
			})]
		})]
	});
});

//#endregion
export { VerifyTOTP };
//# sourceMappingURL=VerifyTOTP.js.map