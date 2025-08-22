from django_cron import CronJobBase, Schedule
from django.core.management import call_command


class ProcessSsls(CronJobBase):
    """Process SSLs.

    This CRON class handles the SSL activation for newly added websites as well as it takes care of the
    SSL certificate renewals.

    Attributes:
        schedule (object): The schedule of this CRON class. It will execute every X minutes.
        code (str): A unique string to distinguish this CRON class among others.
    """
    schedule = Schedule(run_every_mins=10)
    code = 'fastcp.proces_certs'

    def do(self):
        """Executes the logic.

        In this method, we write the CRON job task or the logic.
        """
        call_command('activate-ssl')
