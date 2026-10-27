import dayjs from "dayjs";
import duration from "dayjs/plugin/duration";
import type {
	Workspace,
	WorkspaceBuildDeadlineChange,
} from "#/api/typesGenerated";
import * as Mocks from "#/testHelpers/entities";
import {
	deadlineChangeMessage,
	deadlineExtensionMax,
	deadlineExtensionMin,
	extractTimezone,
	getMaxDeadline,
	getMaxDeadlineChange,
	getMinDeadline,
	quietHoursDisplay,
	stripTimezone,
} from "./schedule";

dayjs.extend(duration);
const now = dayjs();
const startTime = dayjs(Mocks.MockWorkspaceBuild.updated_at);

describe("util/schedule", () => {
	describe("stripTimezone", () => {
		it.each<[string, string]>([
			["CRON_TZ=Canada/Eastern 30 9 1-5", "30 9 1-5"],
			["CRON_TZ=America/Central 0 8 1,2,4,5", "0 8 1,2,4,5"],
			["30 9 1-5", "30 9 1-5"],
		])("stripTimezone(%p) returns %p", (input, expected) => {
			expect(stripTimezone(input)).toBe(expected);
		});
	});

	describe("extractTimezone", () => {
		it.each<[string, string]>([
			["CRON_TZ=Canada/Eastern 30 9 1-5", "Canada/Eastern"],
			["CRON_TZ=America/Central 0 8 1,2,4,5", "America/Central"],
			["30 9 1-5", "UTC"],
		])("extractTimezone(%p) returns %p", (input, expected) => {
			expect(extractTimezone(input)).toBe(expected);
		});
	});

	describe("maxDeadline", () => {
		const workspace: Workspace = {
			...Mocks.MockWorkspace,
			latest_build: {
				...Mocks.MockWorkspaceBuild,
				deadline: startTime.add(8, "hours").utc().format(),
			},
		};
		it("should be 24 hours from the workspace start time", () => {
			const delta = getMaxDeadline(workspace).diff(startTime);
			expect(delta).toEqual(deadlineExtensionMax.asMilliseconds());
		});
	});

	describe("minDeadline", () => {
		it("should never be less than 30 minutes", () => {
			const delta = getMinDeadline().diff(now);
			expect(delta).toBeGreaterThanOrEqual(
				deadlineExtensionMin.asMilliseconds(),
			);
		});
	});

	describe("getMaxDeadlineChange", () => {
		it("should return the number of hours you can add before hitting the max deadline", () => {
			const deadline = dayjs();
			const maxDeadline = dayjs().add(1, "hour").add(40, "minutes");
			// you can only add one hour even though the max is 1:40 away
			expect(getMaxDeadlineChange(deadline, maxDeadline)).toEqual(1);
		});

		it("should return the number of hours you can subtract before hitting the min deadline", () => {
			const deadline = dayjs().add(2, "hours").add(40, "minutes");
			const minDeadline = dayjs();
			// you can only subtract 2 hours even though the min is 2:40 less
			expect(getMaxDeadlineChange(deadline, minDeadline)).toEqual(2);
		});
	});

	describe("deadlineChangeMessage", () => {
		beforeEach(() => {
			vi.useFakeTimers();
			vi.setSystemTime(new Date("2025-07-15T14:30:00.000Z"));
		});

		afterEach(() => {
			vi.useRealTimers();
		});

		const changedAt = "2025-07-15T14:28:00.000Z";

		it.each<[WorkspaceBuildDeadlineChange, string | undefined]>([
			[
				{
					reason: "activity",
					changed_at: changedAt,
					activity: { sources: ["Cursor", "SSH", "Zed"] },
				},
				"Activity from Cursor, SSH, and Zed extended this deadline 2 minutes ago.",
			],
			[
				{
					reason: "activity",
					changed_at: changedAt,
					activity: { sources: ["Cursor", "SSH", "Zed", "SFTP"] },
				},
				"Activity from Cursor, SSH, Zed, and 1 more extended this deadline 2 minutes ago.",
			],
			[
				{ reason: "user_changed", changed_at: changedAt },
				"A user changed this deadline 2 minutes ago.",
			],
			[
				{ reason: "autostop_disabled", changed_at: changedAt },
				"A user turned off autostop 2 minutes ago.",
			],
			[
				{ reason: "template_schedule", changed_at: changedAt },
				"A schedule change on the Test Template template set this deadline 2 minutes ago.",
			],
			[{ reason: "build", changed_at: changedAt }, undefined],
			[
				{ reason: "activity", changed_at: changedAt },
				"Activity extended this deadline 2 minutes ago.",
			],
		])("explains %j", (change, expected) => {
			expect(deadlineChangeMessage(change, Mocks.MockTemplate)).toBe(expected);
		});

		it("falls back to the template name", () => {
			expect(
				deadlineChangeMessage(
					{ reason: "template_schedule", changed_at: changedAt },
					{ ...Mocks.MockTemplate, display_name: "" },
				),
			).toBe(
				`A schedule change on the ${Mocks.MockTemplate.name} template set this deadline 2 minutes ago.`,
			);
		});
	});

	describe("quietHoursDisplay", () => {
		it("midnight in Poland", () => {
			const quietHoursStart = quietHoursDisplay(
				"pl",
				"00:00",
				"Australia/Sydney",
				new Date("2023-09-06T15:00:00.000+10:00"),
			);

			expect(quietHoursStart).toBe(
				"00:00 tomorrow (in 9 hours) in Australia/Sydney",
			);
		});
		it("five o'clock today in Sweden", () => {
			const quietHoursStart = quietHoursDisplay(
				"sv",
				"17:00",
				"Europe/London",
				new Date("2023-09-06T15:00:00.000+10:00"),
			);

			expect(quietHoursStart).toBe(
				"17:00 today (in 11 hours) in Europe/London",
			);
		});
		it("five o'clock today in Finland", () => {
			const quietHoursStart = quietHoursDisplay(
				"fl",
				"17:00",
				"Europe/London",
				new Date("2023-09-06T15:00:00.000+10:00"),
			);

			expect(quietHoursStart).toBe(
				"5:00 PM today (in 11 hours) in Europe/London",
			);
		});
		it("lunch tomorrow in England", () => {
			const quietHoursStart = quietHoursDisplay(
				"en",
				"13:00",
				"US/Central",
				new Date("2023-09-06T08:00:00.000+10:00"),
			);

			expect(quietHoursStart).toBe(
				"1:00 PM tomorrow (in 20 hours) in US/Central",
			);
		});
	});
});
